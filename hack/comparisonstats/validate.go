package main

import (
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

func validateBinaries(binpaths []string) error {
	for _, binpath := range binpaths {
		_, err := os.Stat(binpath)
		if err != nil {
			return err
		}
	}
	return nil

}

// TODO add devIterations validation, must be >0

func validateExampleAppNameAndSrcFile(exampleAppName, exampleSrcFile string) error {
	_, err := os.Stat(filepath.Join("../../examples/", exampleAppName))
	if err != nil {
		return err
	}
	_, err = os.Stat(filepath.Join("../../examples/", exampleAppName, exampleSrcFile))
	if err != nil {
		return err
	}
	return nil
}

func validateArgs(args []string) error {
	if len(args) < numBinaries+1 {
		logrus.Fatalf("comparisonstats expects input of the form: timer-comparison /usr/bin/bin1 /usr/bin/bin2 microservices output.txt")
	}

	if err := validateBinaries(args[1 : len(args)-2]); err != nil {
		return err
	}
	if err := validateExampleAppNameAndSrcFile(args[len(args)-2], args[len(args)-1]); err != nil {
		return err
	}

	return nil
}
