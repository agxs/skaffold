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

package inspect

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/config"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/inspect"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/util"
	diff "github.com/shazow/go-diff"
	"github.com/sirupsen/logrus"
)

type RecommendedConfig []Recommendation

// stringEquals vs regexp is for speed, currently only have stringEquals example
func (rc *RecommendedConfig) SuggestRecsViaStringEquals(d Dockerfile) (*[]Recommendation, error) {
	recs := &[]Recommendation{}
	for _, rec := range *rc {
		// logrus.Infof("Looking for %s in %s", rec.FlaggedConfig.ConfigTemplate, d.Text)
		if idx := strings.Index(strings.ToUpper(d.Text), strings.ToUpper(rec.FlaggedConfig.ConfigTemplate)); idx != -1 {
			rec.FlaggedConfig.TextStartIndex = idx
			rec.FlaggedConfig.Path = d.AbsPath
			rec.FlaggedConfig.TextEndIndex = idx + len(rec.FlaggedConfig.ConfigTemplate)
			rec.RecommendedConfig.Path = d.AbsPath
			*recs = append(*recs, rec)
		}

		// can get linenumber with scanner technique, see if this is needed for Cloud Code UX
		// Splits on newlines by default.
		// NOTE: this method will not work with multi-line checks as is
		// NOTE: if VS CODE can just use index to highlight line this might not be worth doing
		// scanner := bufio.NewScanner(strings.NewReader(d.Text)) // TODO(aaron-prindle) can do this directly from os.Open instead of going file -> string -> reader?
		// line := 1
		// for scanner.Scan() {
		// 	if idx := strings.Index(strings.ToUpper(scanner.Text()), strings.ToUpper(rec.FlaggedConfig.ConfigTemplate)); idx != -1 {
		// 		rec.FlaggedConfig.TextIndex = [2]int{line, idx}
		// 		rec.FlaggedConfig.Path = d.AbsPath
		// 		rec.RecommendedConfig.TextIndex = [2]int{line, idx}
		// 		rec.RecommendedConfig.Path = d.AbsPath
		// 		// TODO(aaron-prindle) do something special w/ action:noop recommendation?
		// 		*recs = append(*recs, rec)

		// 	}
		// 	line++
		// }
	}
	return recs, nil
}

var StringEqualsRecs = RecommendedConfig{
	Recommendation{
		FlaggedConfig: ConfigOpt{ConfigTemplate: "Copy . .", Action: Replace},
		RecommendedConfig: ConfigOpt{ConfigTemplate: "Copy $MIN_SET_OF_NECESSARY_SOURCE_FILES_FOR_IMAGE .", Explanation: "Found 'COPY . .', this is possibly a docker anti-pattern and has the " +
			"potential to dramatically slow 'skaffold dev' down by having skaffold watch all files in a directory for changes. If you notice skaffold rebuilding images unnecessarily when non-image-critical files are " +
			"modified, consider changing this to `COPY $MIN_SET_OF_NECESSARY_SOURCE_FILES_FOR_IMAGE .` for each required source file instead of " +
			"using 'COPY . .'. See TODO:ADD_A_LINK_HERE"}}, // TODO(aaron-prindle) add a link there to docker post
	Recommendation{
		FlaggedConfig:     ConfigOpt{ConfigTemplate: "Testing what only flagged recommendation w/ no rec looks like", Action: Replace},
		RecommendedConfig: ConfigOpt{Action: Noop},
	},
}

// perhaps can have a function callback that takes in dependant artifacts and outputs a subset w/ a filter?
type Action int

const (
	Replace Action = iota
	Add
	Delete
	Noop
)

func (a Action) String() string {
	return [...]string{"Replace", "Add", "Delete", "Noop"}[a]
}

type ConfigOpt struct {
	Action Action
	// Severity?
	Path           string
	ConfigTemplate string
	TextStartIndex int
	TextEndIndex   int
	Explanation    string // also add Suggestion field?  But suggestion will be another piece of config most likely?  maybe not?
}

type Recommendation struct {
	FlaggedConfig     ConfigOpt
	RecommendedConfig ConfigOpt
}

func generateRecommendationDiff(d Dockerfile, r *Recommendation) string {
	// TODO(aaron-prindle) FIX - as is, the indexes will change over time which makes this not work for multiple changes (at least if you did this repeatedly)
	// one hack would be to redo the recommendation finder or do the text gen in the finder loop
	s := d.Text[:r.FlaggedConfig.TextStartIndex] + r.RecommendedConfig.ConfigTemplate + d.Text[r.FlaggedConfig.TextEndIndex:]
	// original Dockerfile
	a := diff.Object{
		ReadSeeker: strings.NewReader(d.Text),
		Path:       d.RelPath + ".orig",
	}
	// recommended Dockerfile
	b := diff.Object{
		ReadSeeker: strings.NewReader(s),
		Path:       d.RelPath,
	}

	differ := diff.DefaultDiffer()
	out := bytes.Buffer{}
	w := diff.Writer{
		Writer:    &out,
		Differ:    differ,
		SrcPrefix: "",
		DstPrefix: "",
	}
	w.Diff(a, b)

	// remove all ^index lines as they are not needed (we do not need sha or mode related information which the diff lib we are using add in the output)
	re := regexp.MustCompile("(?m)[\r\n]+^index.*$")
	res := re.ReplaceAllString(out.String(), "")
	return res
}

type dockerfileRecsList struct {
	DockerfileRecs []Recommendation `json:"dockerfileRecs"`
	// TODO(aaron-prindle) FIX - hack is below for keeping dockerfile text.  Shouldn't work like this ideally
	Dockerfiles []Dockerfile `json:"dockerfiles"`
}

func PrintDockerfilesList(ctx context.Context, out io.Writer, opts inspect.Options) error {
	formatter := inspect.OutputFormatter(out, opts.OutFormat)
	cfgs, err := inspect.GetConfigSet(ctx, config.SkaffoldOptions{ConfigurationFile: opts.Filename, ConfigurationFilter: opts.Modules, RepoCacheDir: opts.RepoCacheDir})
	if err != nil {
		return formatter.WriteErr(err)
	}

	l := &dockerfileRecsList{}
	// TODO(aaron-prindle) avoid doing the actual docker init and then going through the artifacts to get the Dockerfiles, instead do it more directly (currently a bit hacky)
	seen := map[string]bool{}
	workdir, err := util.RealWorkDir()
	if err != nil {
		return err
	}
	for _, c := range cfgs {
		for _, a := range c.Build.Artifacts {
			if a.DockerArtifact != nil {
				fp := filepath.Join(workdir, a.Workspace, a.DockerArtifact.DockerfilePath)
				if _, ok := seen[fp]; ok {
					continue
				}
				seen[fp] = true
				logrus.Infof("Attempting to open Dockerfile %s", fp)
				b, err := ioutil.ReadFile(fp)
				if err != nil {
					return err
				}
				d := Dockerfile{fp, filepath.Join(a.Workspace, a.DockerArtifact.DockerfilePath), string(b)}
				recs, err := getDockerfileRecommendations(d)
				if err != nil {
					return err
				}
				l.DockerfileRecs = append(l.DockerfileRecs, *recs...)
				l.Dockerfiles = append(l.Dockerfiles, d)
			}
		}
	}
	// get original file
	// get new file with recommendation(s)? <---- not sure how to hande this part...
	// how to handle warnings?
	// generate 'diff' style diff between files
	// in actual default/stdout mode, add a preamble series of explanations before each file (or all at the top?)
	// use "diff" command for now?

	for i := range l.DockerfileRecs {
		// logrus.Info(generateRecommendationDiff(l.Dockerfiles[i], &l.DockerfileRecs[i]))
		fmt.Println(generateRecommendationDiff(l.Dockerfiles[i], &l.DockerfileRecs[i]))
	}
	return nil
	// return formatter.Write(l)
}

type Dockerfile struct {
	AbsPath string
	RelPath string
	Text    string
}

// TODO(aaron-prindle) FIX-hack, added Dockerfile param as a hack
func getDockerfileRecommendations(d Dockerfile) (*[]Recommendation, error) {
	recs := []Recommendation{}
	stringEqualsRecs, err := StringEqualsRecs.SuggestRecsViaStringEquals(d)
	if err != nil {
		return nil, err
	}
	recs = append(recs, *stringEqualsRecs...)
	regexpRecs, err := regexpRecommendations()
	if err != nil {
		return nil, err
	}
	recs = append(recs, *regexpRecs...)
	graphRecs, err := graphRecommendations()
	if err != nil {
		return nil, err
	}
	recs = append(recs, *graphRecs...)
	return &recs, nil
}

func regexpRecommendations() (*[]Recommendation, error) {
	return &[]Recommendation{}, nil
}

func graphRecommendations() (*[]Recommendation, error) {
	return &[]Recommendation{}, nil
}
