package lockfile

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

type NpmLockDependency struct {
	// For an aliased package, Version is like "npm:[name]@[version]"
	Version      string                       `json:"version"`
	Dependencies map[string]NpmLockDependency `json:"dependencies,omitempty"`

	Dev      bool `json:"dev,omitempty"`
	Optional bool `json:"optional,omitempty"`
}

type NpmLockPackage struct {
	// For an aliased package, Name is the real package name
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Resolved     string            `json:"resolved"`
	Dependencies map[string]string `json:"dependencies"`

	Dev         bool `json:"dev,omitempty"`
	DevOptional bool `json:"devOptional,omitempty"`
	Optional    bool `json:"optional,omitempty"`
}

type NpmLockfile struct {
	Version int `json:"lockfileVersion"`
	// npm v1- lockfiles use "dependencies"
	Dependencies map[string]NpmLockDependency `json:"dependencies"`
	// npm v2+ lockfiles use "packages"
	Packages map[string]NpmLockPackage `json:"packages,omitempty"`
}

const NpmEcosystem Ecosystem = "npm"

func pkgDetailsMapToSlice(m map[string]PackageDetails) []PackageDetails {
	details := make([]PackageDetails, 0, len(m))

	for _, detail := range m {
		details = append(details, detail)
	}

	return details
}

func mergePkgDetailsMap(m1 map[string]PackageDetails, m2 map[string]PackageDetails) map[string]PackageDetails {
	details := map[string]PackageDetails{}

	for name, detail := range m1 {
		details[name] = detail
	}

	for name, detail := range m2 {
		details[name] = detail
	}

	return details
}

func (dep NpmLockDependency) depGroups() []string {
	if dep.Dev && dep.Optional {
		return []string{"dev", "optional"}
	}
	if dep.Dev {
		return []string{"dev"}
	}
	if dep.Optional {
		return []string{"optional"}
	}

	return nil
}

func parseNpmLockDependencies(dependencies map[string]NpmLockDependency) map[string]PackageDetails {
	details := map[string]PackageDetails{}

	for name, detail := range dependencies {
		if detail.Dependencies != nil {
			details = mergePkgDetailsMap(details, parseNpmLockDependencies(detail.Dependencies))
		}

		version := detail.Version
		finalVersion := version
		commit := ""

		// If the package is aliased, get the name and version
		if strings.HasPrefix(detail.Version, "npm:") {
			i := strings.LastIndex(detail.Version, "@")
			name = detail.Version[4:i]
			finalVersion = detail.Version[i+1:]
		}

		// we can't resolve a version from a "file:" dependency
		if strings.HasPrefix(detail.Version, "file:") {
			finalVersion = ""
		} else {
			commit = tryExtractCommit(detail.Version)

			// if there is a commit, we want to deduplicate based on that rather than
			// the version (the versions must match anyway for the commits to match)
			//
			// we also don't actually know what the "version" is, so blank it
			if commit != "" {
				finalVersion = ""
				version = commit
			}
		}

		details[name+"@"+version] = PackageDetails{
			Name:      name,
			Version:   finalVersion,
			Ecosystem: NpmEcosystem,
			CompareAs: NpmEcosystem,
			Commit:    commit,
			DepGroups: detail.depGroups(),
		}
	}

	return details
}

func extractNpmPackageName(name string) string {
	maybeScope := path.Base(path.Dir(name))
	pkgName := path.Base(name)

	if strings.HasPrefix(maybeScope, "@") {
		pkgName = maybeScope + "/" + pkgName
	}

	return pkgName
}

func (pkg NpmLockPackage) depGroups() []string {
	if pkg.Dev {
		return []string{"dev"}
	}
	if pkg.Optional {
		return []string{"optional"}
	}
	if pkg.DevOptional {
		return []string{"dev", "optional"}
	}

	return nil
}

func parseNpmLockPackages(packages map[string]NpmLockPackage) map[string]PackageDetails {
	details := map[string]PackageDetails{}

	for namePath, detail := range packages {
		if namePath == "" {
			continue
		}

		finalName := detail.Name
		if finalName == "" {
			finalName = extractNpmPackageName(namePath)
		}

		finalVersion := detail.Version

		commit := tryExtractCommit(detail.Resolved)

		// if there is a commit, we want to deduplicate based on that rather than
		// the version (the versions must match anyway for the commits to match)
		if commit != "" {
			finalVersion = commit
		}

		details[finalName+"@"+finalVersion] = PackageDetails{
			Name:      finalName,
			Version:   detail.Version,
			Ecosystem: NpmEcosystem,
			CompareAs: NpmEcosystem,
			Commit:    commit,
			DepGroups: detail.depGroups(),
		}
	}

	return details
}

func parseNpmLock(lockfile NpmLockfile) map[string]PackageDetails {
	if lockfile.Packages != nil {
		return parseNpmLockPackages(lockfile.Packages)
	}

	return parseNpmLockDependencies(lockfile.Dependencies)
}

type NpmLockExtractor struct{}

func (e NpmLockExtractor) ShouldExtract(path string) bool {
	return filepath.Base(path) == "package-lock.json"
}

func (e NpmLockExtractor) Extract(f DepFile) ([]PackageDetails, error) {
	var parsedLockfile *NpmLockfile

	err := json.NewDecoder(f).Decode(&parsedLockfile)

	if err != nil {
		return []PackageDetails{}, fmt.Errorf("could not extract from %s: %w", f.Path(), err)
	}

	return pkgDetailsMapToSlice(parseNpmLock(*parsedLockfile)), nil
}

var _ Extractor = NpmLockExtractor{}

//nolint:gochecknoinits
func init() {
	registerExtractor("package-lock.json", NpmLockExtractor{})
}

func ParseNpmLock(pathToLockfile string) ([]PackageDetails, error) {
	return extractFromFile(pathToLockfile, NpmLockExtractor{})
}
