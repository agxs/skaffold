/*
Copyright 2021 The Skaffold Authors

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

package lint

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/kustomize/kyaml/yaml"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/config"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/docker"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/output/log"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/parser"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/runner/runcontext"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/util"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/version"
)

// for testing
var getConfigSet = parser.GetConfigSet

var SkaffoldYamlLinters = []Linter{
	&RegExpLinter{},
	&YamlFieldLinter{},
}

// func getDockerBuildConfig(ws string, artifact string, a *latestV1.CustomArtifact) docker.BuildConfig {
// 	dockerfile := a.Dependencies.Dockerfile
// 	return docker.NewBuildConfig(ws, artifact, dockerfile.Path, dockerfile.BuildArgs)
// }

func getDockerfileFileDependencies(ws string, absPath string, cfg docker.Config) ([]string, error) {
	buildArgs := map[string]*string{}

	return docker.GetDependencies(context.TODO(),
		// TODO(aaron-prindle) should I be using an absPath?  Seems to work ...
		// TODO(aaron-prindle) not sure what to do with "dummy-artifact-name", filed not relevant for my use case IIUC
		docker.NewBuildConfig(ws, "dummy-artifact-name", absPath, buildArgs), nil)
}

// ideas for outputting information into explanation field

// other ideas
// one giant struct + go templates

// **Best idea so far:
// need function that takes linterInputs? and converts them to map of strings?
// store a k,v map and use the map + go templates

var skaffoldYamlRules = []Rule{
	{
		Filter: YamlFieldFilter{
			Filter: yaml.FieldMatcher{Name: "apiVersion", StringRegexValue: fmt.Sprintf("[^%s]", version.Get().ConfigVersion)},
		},
		RuleID:   SkaffoldYamlAPIVersionOutOfDate,
		RuleType: YamlFieldLintRule,
		ExplanationTemplate: fmt.Sprintf("Found 'apiVersion' field with value that is not the latest skaffold apiVersion. Modify the apiVersion to the latest version: `apiVersion: %s` "+
			"or run the 'skaffold fix' command to have skaffold upgrade this for you.", version.Get().ConfigVersion),
	},
	{
		// TODO(aaron-prindle) currently this does not meet all cases (eg: portForward there but no "port:")
		// TODO(aaron-prindle) currently this does not meet all cases, doesn't check that there are deployed resources
		// TODO(aaron-prindle) output should be exactly what is required (example port #, deployment name, etc), not a generic message
		Filter: YamlFieldFilter{
			Filter:      yaml.FieldMatcher{Name: "portForward"},
			InvertMatch: true,
		},
		RuleID:   SkaffoldYamlUseStaticPort,
		RuleType: YamlFieldLintRule,
		// TODO(aaron-prindle) fill in the X,Y,Z reasons for static port usage
		ExplanationTemplate: "It is a skaffold best practice to specify a static port (vs skaffold dynamically choosing one) for port forwarding on resources skaffold deploys.  This is helpful because X,Y,Z. " +
			"It is recommended to add the following stanza at the end of your skaffold.yaml for each deployed resource\n" + `portForward:
- resourceType: <TYPE-OF-DEPLOYED-RESOURCE> // ex: deployment
  resourceName: <NAME-OF-DEPLOYED-RESOURCE> // ex: nginx-deployment
  port: <PORT-#-OR-NAMED-PORT-FOR-DEPLOYED-RESOURCE> // ex: 8080 or http
  localPort: <LOCAL-PORT-FOR-DEPLOYED-RESOURCE>// ex: 9001`,
		// LintConditions: []func(string) bool{
		// 	func(s string) bool {

		// 		return true //TODO(aaron-prindle) tmp hack, remove
		// 	},
		ExplanationPopulator: func(lintInputs LintInputs) (explanationInfo, error) {
			// TODO(aaron-prindle) not sure this will work properly w/ modules (at least recommendation likely we won't be on correct skaffold.yaml file)
			// need a mapping of skaffold.yaml -> specific manifests to know where to put these statements
			// might work still though...
			for _, pattern := range lintInputs.SkaffoldConfig.Deploy.KubectlDeploy.Manifests {
				// log.Entry(context.TODO()).Infof("m: %v\n", m)

				// NOTE: pattern is a pattern that can have wildcards, eg: leeroy-app/kubernetes/*
				if util.IsURL(pattern) {
					log.Entry(context.TODO()).Infof("skaffold lint found url manifest when processing rule %d and is skipping lint rules for: %s", SkaffoldYamlUseStaticPort, pattern)
					continue
				}
				// filepaths are all absolute from config parsing step via tags.MakeFilePathsAbsolute
				expanded, err := filepath.Glob(pattern)
				if err != nil {
					return explanationInfo{}, err
					// TODO(aaron-prindle) support returning multiple errors?
					// errs = append(errs, err)
				}

				for _, relPath := range expanded {
					b, err := ioutil.ReadFile(relPath)
					if err != nil {
						return explanationInfo{}, nil
					}
					// TODO(aaron-prindle) finish this ...
				}
			}
			return explanationInfo{}, nil
		},
	},

	// information required for full text replacement (requires parsing deployment yaml)
	// resourceType (ex: resourceType: deployment)
	// resourceName (ex: resourceName: leeroy-app)
	// port (ex: port: 8080 OR port: http)
	// localPort (ex: localPort: 9000)

	// how to get info needed
	// iterate over deployments
	//   - get deployment type
	//   - get deployment name
	//   - pick a port that is open, add a comment stating that the user should update this if needed

	// TODO(aaron-prindle) this suggestion needs to be mapped to the "build:" section (currently at end of file)
	{
		Filter: YamlFieldFilter{
			Filter:    yaml.Get("build"), // TODO(aaron-prindle) check if there is a way to only get the first match w/ only a Filter  (wasn't able to find any method...)
			FieldOnly: "build",
		},
		RuleID:   SkaffoldYamlSyncPython,
		RuleType: YamlFieldLintRule,
		ExplanationTemplate: "Found files with extension *.py in docker build container image that should be " +
			"synced (via skaffold’s container rsync) vs fully rebuilding the image when modified. It is recommended " +
			"to put the following stanza in the `build` section of the flagged skaffold.yaml:\n" +

			`    sync:
       manual:
       # Syncs the local *.py files beneath the {{index .FieldMap "src"}} folder to the identical folder in the container
       - src: {{index .FieldMap "src"}}	# these are the local file(s) skaffold will sync to the container 
       dest: . # this is the container folder the local *.py files will be synced to.  NOTE: verify this is correct (dir exists in container, etc.), change if needed` + "\n",
		ExplanationPopulator: func(lintInputs LintInputs) (explanationInfo, error) {
			// TODO(aaron-prindle) verify codepath does not need to handle edge cases (accounted for by order of operations + Filters, LintConditions, etc.)
			// TODO(aaron-prindle) this needs to handle multiple dirs, "src" needs to be an array
			// TODO(aaron-prindle) analyze the Dockerfile COPY commands to get the correct 'dest'
			for _, deps := range lintInputs.DockerfileToDepMap {
				for _, dep := range deps {
					if strings.HasSuffix(dep, ".py") {
						return explanationInfo{
							FieldMap: map[string]string{
								"src": filepath.Join(filepath.Dir(dep), "**", "*.py"), // TODO(aaron-prindle) verify globstar syntax is ok vs just syncing all files in a list
							},
						}, nil
					}
				}
			}
			return explanationInfo{}, nil
		},
		LintConditions: []func(LintInputs) bool{
			func(lintInputs LintInputs) bool {
				linter := &YamlFieldLinter{}
				recs, err := linter.Lint(lintInputs, &[]Rule{
					{
						RuleType: YamlFieldLintRule,
						Filter: YamlFieldFilter{
							Filter:      yaml.Lookup("build", "sync"),
							InvertMatch: true,
						},
					},
				})
				if err != nil {
					// TODO(aaron-prindle) output this error to logging
					return false
				}
				if len(*recs) > 0 {
					return true
				}
				return false
			},
			func(lintInputs LintInputs) bool {
				for _, deps := range lintInputs.DockerfileToDepMap {
					for _, dep := range deps {
						if strings.HasSuffix(dep, ".py") {
							return true
						}
					}
				}
				return false
			},
		},
	},
	// ideas on how this could actually be implemented
	// LintConditions
	//  - if no current 'infer' stanza
	//
	//  - if docker build
	//    (if Dockerfile found?)
	//  - if *.txt or *.html artifacts found in as artifacts in a docker graph
	//    - highlight build stanza and say to add
	/*
		   sync:
		     # Sync files with matching suffixes directly into container via rsync:
		     - '*.txt'
			 - '*.html'
	*/
	// TODO(aaron-prindle) verify rsync^^^^^ is correct term to use
}

func GetSkaffoldYamlsLintResults(ctx context.Context, opts Options, runCtx *runcontext.RunContext) (*[]Result, error) {
	cfgs, err := getConfigSet(ctx, config.SkaffoldOptions{
		ConfigurationFile:   opts.Filename,
		ConfigurationFilter: opts.Modules,
		RepoCacheDir:        opts.RepoCacheDir,
		Profiles:            opts.Profiles,
	})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	workdir, err := realWorkDir()
	if err != nil {
		return nil, err
	}
	l := []Result{}
	dockerfileDepMap := map[string][]string{}
	for _, c := range cfgs {
		b, err := ioutil.ReadFile(c.SourceFile)
		if err != nil {
			return nil, err
		}
		skaffoldyaml := ConfigFile{
			AbsPath: c.SourceFile,
			RelPath: strings.TrimPrefix(c.SourceFile, workdir),
			Text:    string(b),
		}
		// TODO(aaron-prindle) iterate over all Dockerfiles....
		// =======
		for _, a := range c.Build.Artifacts {
			if a.DockerArtifact != nil {
				ws := filepath.Join(workdir, a.Workspace)
				fp := filepath.Join(ws, a.DockerArtifact.DockerfilePath)
				if _, ok := seen[fp]; ok {
					continue
				}
				seen[fp] = true
				deps, err := getDockerfileFileDependencies(ws, fp, runCtx)
				if err != nil {
					// TODO(aaron-prindle) support array of errors
					return nil, err
				}
				log.Entry(context.TODO()).Infof("dockerfile: %s, deps: %v", fp, deps)
				dockerfileDepMap[fp] = deps
			}
		}

		results := []Result{}
		for _, r := range SkaffoldYamlLinters {
			recs, err := r.Lint(LintInputs{
				ConfigFile:         skaffoldyaml,
				DockerfileToDepMap: dockerfileDepMap, // TODO(aaron-prindle) decide if this should be derived from SkaffoldConfig vs explicit
				SkaffoldConfig:     c,
			}, &skaffoldYamlRules)
			if err != nil {
				return nil, err
			}
			results = append(results, *recs...)
		}
		l = append(l, results...)
	}
	return &l, nil
}

// for _, pattern := range c.Deploy.KubectlDeploy.Manifests {
// 	// NOTE: pattern is a pattern that can have wildcards, eg: leeroy-app/kubernetes/*
// 	if util.IsURL(pattern) {
// 		logrus.Infof("skaffold lint found url manifest and is skipping lint rules for: %s", pattern)
// 		continue
// 	}
// 	// filepaths are all absolute from config parsing step via tags.MakeFilePathsAbsolute
// 	expanded, err := filepath.Glob(pattern)
// 	if err != nil {
// 		return nil, err
// 		// TODO(aaron-prindle) support returning multiple errors?
// 		// errs = append(errs, err)
// 	}

// 	for _, relPath := range expanded {
// 		b, err := ioutil.ReadFile(relPath)
// 		if err != nil {
// 			return nil, nil
// 		}
// 		k8syaml := ConfigFile{
// 			AbsPath: filepath.Join(workdir, relPath),
// 			RelPath: relPath,
// 			Text:    string(b),
// 		}
// 		mrs := []MatchResult{}
// 		for _, r := range K8sYamlLinters {
// 			recs, err := r.Lint(k8syaml, &K8sYamlLintRules)
// 			if err != nil {
// 				return nil, err
// 			}
// 			mrs = append(mrs, *recs...)
// 		}
// 		l.K8sYamlLintRules = append(l.K8sYamlLintRules, mrs...)
// 	}
// }
// }
