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
	"context"
	"io"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/config"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/inspect"
)

var BadConfigToSuggestedConfig = map[ConfigStatement]ConfigStatement{
	{ConfigTemplate: "Copy . ."}: {ConfigTemplate: "Copy %s"},
}

type Action int

const (
	Replace Action = iota
	Add
	Delete
)

type ConfigStatement struct {
	Action         Action
	Path           string
	ConfigTemplate string
	TextIndex      [2]int
	Explanation    string // also add Suggestion field?  But suggestion will be another piece of config most likely?  maybe not?
}

type Recommendation struct {
	FlaggedConfiguration     ConfigStatement
	RecommendedConfiguration ConfigStatement
}

type skaffoldYamlRecommendationList struct {
	SkaffoldYamlRecommendations []Recommendation `json:"skaffoldYamls"`
}

// // CustomSkaffoldYaml entries are handled by CustomSkaffoldYaml struct, there is no StructureSkaffoldYaml so structureSkaffoldYamlEntry is required
// type structureSkaffoldYamlEntry struct {
// 	StructureSkaffoldYaml     string   `json:"structureSkaffoldYaml"`
// 	StructureSkaffoldYamlArgs []string `json:"structureSkaffoldYamlArgs"`
// }

func PrintSkaffoldYamlsList(ctx context.Context, out io.Writer, opts inspect.Options) error {
	formatter := inspect.OutputFormatter(out, opts.OutFormat)
	_, err := inspect.GetConfigSet(ctx, config.SkaffoldOptions{ConfigurationFile: opts.Filename, ConfigurationFilter: opts.Modules, RepoCacheDir: opts.RepoCacheDir})
	// cfgs, err := inspect.GetConfigSet(ctx, config.SkaffoldOptions{ConfigurationFile: opts.Filename, ConfigurationFilter: opts.Modules, RepoCacheDir: opts.RepoCacheDir})
	if err != nil {
		return formatter.WriteErr(err)
	}

	l := &skaffoldYamlRecommendationList{SkaffoldYamlRecommendations: []Recommendation{}}
	// for _, c := range cfgs {
	// 	for _, t := range c.SkaffoldYaml {
	// 		for _, ct := range t.CustomSkaffoldYamls {
	// 			l.SkaffoldYamls = append(l.SkaffoldYamls, ct)
	// 		}
	// 		for _, st := range t.StructureSkaffoldYamls {
	// 			l.SkaffoldYamls = append(l.SkaffoldYamls, structureSkaffoldYamlEntry{StructureSkaffoldYaml: st, StructureSkaffoldYamlArgs: t.StructureSkaffoldYamlArgs})
	// 		}
	// 	}
	// }
	return formatter.Write(l)
}
