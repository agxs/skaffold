package types

import (
	"bytes"
	"fmt"
	"math"
	"time"

	"github.com/GoogleContainerTools/skaffold/hack/comparisonstats/util"
)

type ComparisonStats struct {
	BinaryPath            string
	CmdArgs               []string
	BinarySize            int64
	DevIterations         int64
	DevLoopEventDurations *DevLoopTimes
}

// timeslice adapted from https://github.com/jamiealquiza/tachymeter

// timeslice holds time.Duration values.
type timeSlice []time.Duration

// Satisfy sort for timeSlice.
func (p timeSlice) Len() int           { return len(p) }
func (p timeSlice) Less(i, j int) bool { return int64(p[i]) < int64(p[j]) }
func (p timeSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func (ts timeSlice) avg() time.Duration {
	var total time.Duration
	for _, t := range ts {
		total += t
	}
	return time.Duration(int(total) / ts.Len())
}

func (ts timeSlice) stdDev() time.Duration {
	m := ts.avg()
	s := 0.00
	for _, t := range ts {
		s += math.Pow(float64(m-t), 2)
	}
	msq := s / float64(ts.Len())
	return time.Duration(math.Sqrt(msq))
}

// TODO(aaron-prindle) add median?
// 	fmt.Printf("Median  = %.5f\n", samples[int(total*.5)])
func (cs *ComparisonStats) String() string {
	var b bytes.Buffer

	fmt.Fprintln(&b, "==========")
	fmt.Fprintf(&b, "information for %v for %d iterations of %s:\n", cs.BinaryPath, cs.DevIterations, cs.CmdArgs)
	fmt.Fprintf(&b, "binary size: %v\n", util.HumanReadableBytesSizeSI(cs.BinarySize))
	fmt.Fprintf(&b, "initial loop build, deploy, status-check times: %v\n", []time.Duration{
		cs.DevLoopEventDurations.InitialBuildTime, cs.DevLoopEventDurations.InitialDeployTime, cs.DevLoopEventDurations.InitialStatusCheckTime})
	fmt.Fprintf(&b, "inner loop build time avg: %s\n", cs.DevLoopEventDurations.InnerBuildTimes.avg())
	// fmt.Fprintf(&b, "inner loop build times: %v\n", cs.devLoopTimes.InnerBuildTimes)
	fmt.Fprintf(&b, "inner loop build time stdDev: %s\n", cs.DevLoopEventDurations.InnerBuildTimes.stdDev())
	fmt.Fprintf(&b, "inner loop deploy time avg: %s\n", cs.DevLoopEventDurations.InnerDeployTimes.avg())
	// fmt.Fprintf(&b, "inner loop deploy times: %v\n", cs.devLoopTimes.InnerDeployTimes)
	fmt.Fprintf(&b, "inner loop deploy time stdDev: %s\n", cs.DevLoopEventDurations.InnerDeployTimes.stdDev())
	fmt.Fprintf(&b, "inner loop status check time avg: %s\n", cs.DevLoopEventDurations.InnerStatusCheckTimes.avg())
	// fmt.Fprintf(&b, "inner loop status check times: %s\n", cs.devLoopTimes.InnerStatusCheckTimes)
	fmt.Fprintf(&b, "inner loop status check time stdDev: %s\n", cs.DevLoopEventDurations.InnerStatusCheckTimes.stdDev())
	return b.String()
}

type DevLoopTimes struct {
	InitialBuildTime       time.Duration
	InitialDeployTime      time.Duration
	InitialStatusCheckTime time.Duration
	InnerBuildTimes        timeSlice
	InnerDeployTimes       timeSlice
	InnerStatusCheckTimes  timeSlice
}

// Application represents a single test application
type Application struct {
	Name          string            `yaml:"name" yamltags:"required"`
	Context       string            `yaml:"context" yamltags:"required"`
	Dev           Dev               `yaml:"dev" yamltags:"required"`
	DevIterations int64             `yaml:"devIterations" yamltags:"required"`
	Labels        map[string]string `yaml:"labels" yamltags:"required"`
}

// Dev describes necessary info for running `skaffold dev` on a test application
type Dev struct {
	Command string `yaml:"command" yamltags:"required"`
	// UndoCommand string `yaml:"undoCommand,omitempty" yamltags:"required"`
}
