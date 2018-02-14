package supply

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"

	"github.com/cloudfoundry/libbuildpack"
)

type Manifest interface {
	AllDependencyVersions(string) []string
	InstallDependency(libbuildpack.Dependency, string) error
}

type Stager interface {
	BuildDir() string
	DepDir() string
	DepsDir() string
	DepsIdx() string
	LinkDirectoryInDepDir(string, string) error
}

type Command interface {
	Execute(string, io.Writer, io.Writer, string, ...string) error
}

type Supplier struct {
	Stager   Stager
	Manifest Manifest
	Command  Command
	Log      *libbuildpack.Logger
}

type Packages struct {
	Packages []Source `yaml:"packages"`
}

type Source struct {
	CranMirror string    `yaml:"cran_mirror"`
	Packages   []Package `yaml:"packages"`
}

type Package struct {
	Name string `yaml:"name"`
}

func New(stager Stager, command Command, manifest Manifest, logger *libbuildpack.Logger) *Supplier {
	return &Supplier{
		Stager:   stager,
		Command:  command,
		Manifest: manifest,
		Log:      logger,
	}
}

func (s *Supplier) Run() error {
	s.Log.BeginStep("Supplying R")

	if err := s.InstallR(); err != nil {
		s.Log.Error("Error installing R: %v", err)
		return err
	}

	if err := s.RewriteRHome(); err != nil {
		s.Log.Error("Error rewriting R_HOME: %v", err)
		return err
	}

	yaml := libbuildpack.NewYAML()
	path_to_ryml := filepath.Join(s.Stager.BuildDir(), "r.yml")
	packages_to_install := Packages{}
	if err := yaml.Load(path_to_ryml, &packages_to_install); err != nil {
		return fmt.Errorf("Couldn't load r.yml: %s", err)
	}

	if err := s.InstallPackages(packages_to_install); err != nil {
		s.Log.Error("Error installing packages: %v", err)
		return err
	}

	return nil
}

func (s *Supplier) InstallPackages(packages_to_install Packages) error {
	// Set DEPS_DIR because R needs it to know its R_HOME
	err := os.Setenv("DEPS_DIR", s.Stager.DepsDir())
	if err != nil {
		return fmt.Errorf("Error setting DEPS_DIR to %s: %s", s.Stager.DepsDir(), err)
	}

	isAlphaOrDot := regexp.MustCompile(`^[A-Za-z0-9.]+$`).MatchString
	for _, src := range packages_to_install.Packages {
		for _, pckg := range src.Packages {
			if !isAlphaOrDot(pckg.Name) {
				return fmt.Errorf("Invalid package name (%s). Only letters, numbers, and periods are allowed.")
			}
			err = s.Command.Execute(s.Stager.BuildDir(), s.Log.Output(), s.Log.Output(), "R", "--vanilla", "-e", fmt.Sprintf("install.packages(\"%s\",repos=\"%s\",dependencies=TRUE)", pckg.Name, src.CranMirror))
			if err != nil {
				return fmt.Errorf("Error while installing %s from %s: %s", pckg.Name, src.CranMirror, err)
			}
		}
	}
	return nil
}

// R> .libPaths()
func (s *Supplier) RewriteRHome() error {
	path := filepath.Join(s.Stager.DepDir(), "r", "bin", "R")
	body, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	body = bytes.Replace(body, []byte("/usr/local/lib/R"), []byte(filepath.Join("$DEPS_DIR", s.Stager.DepsIdx(), "r")), -1)

	return ioutil.WriteFile(path, body, 0755)
}

func (s *Supplier) InstallR() error {
	versions := s.Manifest.AllDependencyVersions("r")
	ver, err := libbuildpack.FindMatchingVersion("x", versions)
	if err != nil {
		return err
	}

	if err := s.Manifest.InstallDependency(libbuildpack.Dependency{Name: "r", Version: ver}, filepath.Join(s.Stager.DepDir(), "r")); err != nil {
		return err
	}

	if err := s.Stager.LinkDirectoryInDepDir(filepath.Join(s.Stager.DepDir(), "r", "bin"), "bin"); err != nil {
		return err
	}
	return s.Stager.LinkDirectoryInDepDir(filepath.Join(s.Stager.DepDir(), "r", "lib"), "lib")
}