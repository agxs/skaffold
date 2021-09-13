/*
Copyright 2020 The Skaffold Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/blang/semver"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/homedir"

	kctx "github.com/GoogleContainerTools/skaffold/pkg/skaffold/kubernetes/context"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/output/log"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/util"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/version"
)

var (
	GetClient = getClient
	// --user flag introduced in 1.18.0
	minikubeVersionWithUserFlag = semver.MustParse("1.18.0")
	// `minikube profile list --light` introduced in 1.18.0
	minikubeVersionWithProfileLightFlag = semver.MustParse("1.18.0")
)

// To override during tests
var (
	FindMinikubeBinary    = minikubeBinary
	getClusterInfo        = kctx.GetClusterInfo
	GetCurrentVersionFunc = getCurrentVersion

	findOnce sync.Once
	mk       = struct {
		err     error // determines if version and path are valid
		version semver.Version
		path    string
	}{}
)

type Client interface {
	// IsMinikube returns true if the given kubeContext maps to a minikube cluster
	IsMinikube(ctx context.Context, kubeContext string) bool
	// MinikubeExec returns the Cmd struct to execute minikube with given arguments
	MinikubeExec(ctx context.Context, arg ...string) (*exec.Cmd, error)
	// UseDockerEnv() returns true if it is recommended to use Minikube's docker-env
	UseDockerEnv(ctx context.Context) bool
}

type clientImpl struct {
	kubeContext string // empty if not resolved
	isMinikube  bool
	profile     *profile // nil if not yet fetched
}

func getClient() Client {
	return &clientImpl{}
}

// IsMinikube returns true if `kubeContext` seems to be a minikube cluster.
// This function is called on several hotpaths so it should be quick.
func (c *clientImpl) IsMinikube(ctx context.Context, kubeContext string) bool {
	if c.kubeContext == kubeContext {
		return c.isMinikube
	}

	c.kubeContext = kubeContext
	c.isMinikube = false // default to not being minikube
	c.profile = nil

	if _, _, err := FindMinikubeBinary(ctx); err != nil {
		log.Entry(context.TODO()).Tracef("Minikube cluster not detected: %v", err)
		return false
	}

	// Although it's extremely unlikely that any other cluster would
	// have the name `minikube`, our checks here are sufficiently quick
	// that we can do a slightly more thorough check.

	kubeConfig, err := kctx.CurrentConfig()
	if err != nil {
		log.Entry(context.TODO()).Tracef("Minikube cluster not detected: %v", err)
		return false
	}
	ktxt, found := kubeConfig.Contexts[kubeContext]
	if !found {
		log.Entry(context.TODO()).Tracef("failed to get cluster info: %v", err)
		return false
	}
	cluster, found := kubeConfig.Clusters[ktxt.Cluster]
	if !found {
		log.Entry(context.TODO()).Debugf("context %q could not be resolved to a cluster", kubeContext)
		return false
	}

	if ext, found := ktxt.Extensions["context_info"]; found && isMinikubeExtension(ext) {
		log.Entry(context.TODO()).Tracef("Minikube cluster detected: context %q has minikube `context_info`", kubeContext)
		c.isMinikube = true
		return true
	}
	if ext, found := ktxt.Extensions["cluster_info"]; found && isMinikubeExtension(ext) {
		log.Entry(context.TODO()).Tracef("Minikube cluster detected: context %q cluster %q has minikube `cluster_info`", kubeContext, ktxt.Cluster)
		c.isMinikube = true
		return true
	}
	if matchClusterCertPath(cluster.CertificateAuthority) {
		log.Entry(context.TODO()).Tracef("Minikube cluster detected: context %q cluster %q has minikube certificate", kubeContext, ktxt.Cluster)
		c.isMinikube = true
		return true
	}
	if err := c.resolveProfile(ctx); err == nil {
		log.Entry(context.TODO()).Tracef("Minikube cluster detected: context %q cluster %q resolved minikube profile", kubeContext, ktxt.Cluster)
		c.isMinikube = true
		return true
	}

	log.Entry(context.TODO()).Tracef("Minikube cluster not detected for context %q", kubeContext)
	return false
}

func (clientImpl) MinikubeExec(ctx context.Context, arg ...string) (*exec.Cmd, error) {
	return minikubeExec(ctx, arg...)
}

func (c *clientImpl) UseDockerEnv(ctx context.Context) bool {
	logrus.Tracef("Checking if build should use `minikube docker-env`")
	if err := c.resolveProfile(ctx); err != nil {
		logrus.Tracef("failed to match minikube profile: %v", err)
		return false
	}
	// Cannot build to the docker daemon with multi-node setups
	return c.profile.Config.KubernetesConfig.ContainerRuntime == "docker" && len(c.profile.Config.Nodes) == 1
}

// resolveProfile finds the matching minikube profile, a profile whose name or serverURL matches
// the selected context or cluster.
func (c *clientImpl) resolveProfile(ctx context.Context) error {
	// early exit if already resolved
	if c.profile != nil {
		return nil
	}

	// TODO: Revisit once https://github.com/kubernetes/minikube/issues/6642 is fixed
	kubeConfig, err := kctx.CurrentConfig()
	if err != nil {
		return err
	}
	ktxt, found := kubeConfig.Contexts[c.kubeContext]
	if !found {
		return fmt.Errorf("no context named %q", c.kubeContext)
	}
	cluster, found := kubeConfig.Clusters[ktxt.Cluster]
	if !found {
		return fmt.Errorf("no cluster %q found for context %q", ktxt.Cluster, c.kubeContext)
	}

	profiles, err := listProfilesLight(ctx)
	if err != nil {
		return fmt.Errorf("minikube profile list: %w", err)
	}

	// when minikube driver is a VM then the node IP should also match the k8s api server url
	serverURL, err := url.Parse(cluster.Server)
	if err != nil {
		logrus.Tracef("invalid server url: %v", err)
		return err
	}
	for _, v := range profiles.Valid {
		for _, n := range v.Config.Nodes {
			if serverURL.Host == fmt.Sprintf("%s:%d", n.IP, n.Port) {
				c.profile = &v
				return nil
			}
		}
	}

	// otherwise check for matching context or cluster name
	for _, v := range profiles.Valid {
		if v.Config.Name == ktxt.Cluster || v.Config.Name == c.kubeContext {
			c.profile = &v
			return nil
		}
	}

	return fmt.Errorf("no valid minikube profile found for %q", c.kubeContext)
}

// listProfilesLight tries to get the minikube profiles as fast as possible.
func listProfilesLight(ctx context.Context) (*profileList, error) {
	args := []string{"profile", "list", "-o", "json"}
	if mk.version.GE(minikubeVersionWithProfileLightFlag) {
		args = append(args, "--light")
	}
	cmd, err := minikubeExec(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("minikube profile list: %w", err)
	}
	out, err := util.RunCmdOut(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("minikube profile list: %w", err)
	}
	var profiles profileList
	if err = json.Unmarshal(out, &profiles); err != nil {
		return nil, fmt.Errorf("unmarshal minikube profile list: %w", err)
	}
	return &profiles, nil
}

func minikubeExec(ctx context.Context, arg ...string) (*exec.Cmd, error) {
	b, v, err := FindMinikubeBinary(ctx)
	if err != nil && !errors.As(err, &versionErr{}) {
		return nil, fmt.Errorf("getting minikube executable: %w", err)
	} else if err == nil && supportsUserFlag(v) {
		arg = append(arg, "--user=skaffold")
	}
	return exec.Command(b, arg...), nil
}

func supportsUserFlag(ver semver.Version) bool {
	return ver.GE(minikubeVersionWithUserFlag)
}

// Retrieves minikube version
func getCurrentVersion(ctx context.Context) (semver.Version, error) {
	cmd := exec.Command("minikube", "version", "--output=json")
	out, err := util.RunCmdOut(ctx, cmd)
	if err != nil {
		return semver.Version{}, err
	}
	minikubeOutput := map[string]string{}
	err = json.Unmarshal(out, &minikubeOutput)
	if v, ok := minikubeOutput["minikubeVersion"]; ok {
		currentVersion, err := version.ParseVersion(v)
		if err != nil {
			return semver.Version{}, err
		}
		return currentVersion, nil
	}
	return semver.Version{}, err
}

func minikubeBinary(ctx context.Context) (string, semver.Version, error) {
	findOnce.Do(func() {
		filename, err := exec.LookPath("minikube")
		if err != nil {
			mk.err = errors.New("unable to lookup minikube executable. Please add it to PATH environment variable")
		}
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			mk.err = fmt.Errorf("unable to find minikube executable. File not found %s", filename)
		}
		mk.path = filename
		if v, err := GetCurrentVersionFunc(ctx); err != nil {
			mk.err = versionErr{err: err}
		} else {
			mk.version = v
		}
	})

	return mk.path, mk.version, mk.err
}

type versionErr struct {
	err error
}

func (v versionErr) Error() string {
	return v.err.Error()
}

// isMinikubeExtension checks if the provided extension is from minikube.
// This extension was introduced with minikube 1.17.
func isMinikubeExtension(extension runtime.Object) bool {
	if extension == nil {
		return false
	}
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(extension)
	if err != nil {
		logrus.Debugf("Unable to decode extension [%T]: %v", extension, err)
		return false
	}
	return m["provider"] == "minikube.sigs.k8s.io"
}

// matchClusterCertPath checks if the cluster certificate for this context is from inside the minikube directory
func matchClusterCertPath(certPath string) bool {
	return certPath != "" && util.IsSubPath(minikubePath(), certPath)
}

// minikubePath returns the path to the user's minikube dir
func minikubePath() string {
	minikubeHomeEnv := os.Getenv("MINIKUBE_HOME")
	if minikubeHomeEnv == "" {
		return filepath.Join(homedir.HomeDir(), ".minikube")
	}
	if filepath.Base(minikubeHomeEnv) == ".minikube" {
		return minikubeHomeEnv
	}
	return filepath.Join(minikubeHomeEnv, ".minikube")
}

type profileList struct {
	Valid   []profile `json:"valid,omitempty"`
	Invalid []profile `json:"invalid,omitempty"`
}

type profile struct {
	Config config
}

type config struct {
	Name string
	// virtualbox, parallels, vmwarefusion, hyperkit, vmware, docker, ssh
	Driver           string
	Nodes            []node
	KubernetesConfig kubernetesConfig
}

type node struct {
	IP   string
	Port int32
}

type kubernetesConfig struct {
	ContainerRuntime string
}
