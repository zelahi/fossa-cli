package pipenv

import (
	"encoding/json"
	"fmt"

	"github.com/fossas/fossa-cli/errors"
	"github.com/fossas/fossa-cli/exec"
	"github.com/fossas/fossa-cli/graph"
	"github.com/fossas/fossa-cli/pkg"
)

// Pipenv defines the interface for all pipenv tool implementations.
type Pipenv interface {
	Deps() (graph.Deps, *errors.Error)
}

// Cmd implements Pipenv by parsing command output.
type Cmd struct {
	Dir   string
	Graph func(dir string) (string, *errors.Error)
}

// dependency is used to unmarshal the output from `pipenv graph --json-tree`.
type dependency struct {
	Package      string `json:"package_name"`
	Resolved     string `json:"installed_version"`
	Target       string `json:"required_version"`
	Dependencies []dependency
}

// New constructs a Pipenv instance that calls the pipenv build tool.
func New(dirname string) Pipenv {
	return Cmd{
		Dir:   dirname,
		Graph: GraphJSON,
	}
}

// Deps returns the dependencies of a pipenv project.
func (p Cmd) Deps() (graph.Deps, *errors.Error) {
	depGraph := graph.Deps{}
	rawJSON, err := p.Graph(p.Dir)
	if err != nil {
		return depGraph, err
	}

	deps, err := getDependencies(rawJSON)
	if err != nil {
		return depGraph, err
	}

	depGraph.Direct = getDirectDeps(deps)
	depGraph.Transitive = getTransitiveDeps(deps)
	return depGraph, nil
}

// GraphJSON returns the output from `pipenv graph --json-tree`.
func GraphJSON(dirname string) (string, *errors.Error) {
	out, _, err := exec.Run(exec.Cmd{
		Name: "pipenv",
		Argv: []string{"graph", "--json-tree"},
		Dir:  dirname,
	})
	if err != nil {
		return "", &errors.Error{
			Cause:           err,
			Type:            errors.Exec,
			Troubleshooting: fmt.Sprintf("Fossa could not run `pipenv graph --json-tree` within the directory `%s`. Try running this command and ensure that Pipenv is installed correctly.", dirname),
			Link:            "https://github.com/fossas/fossa-cli/blob/master/docs/integrations/python.md#strategy-string",
		}
	}
	return out, nil
}

func getDependencies(graphJSON string) ([]dependency, *errors.Error) {
	var depList []dependency
	err := json.Unmarshal([]byte(graphJSON), &depList)
	if err != nil {
		return nil, &errors.Error{
			Cause:           err,
			Type:            errors.Unknown,
			Troubleshooting: fmt.Sprintf("The following output could not be un-marshalled into JSON:\n%s\ntry running the command on your own and check for any errors", string(graphJSON)),
			Link:            "https://pip.pypa.io/en/stable/reference/pip_list",
		}
	}
	return depList, nil
}

func getDirectDeps(depList []dependency) []pkg.Import {
	var imports []pkg.Import
	for _, dep := range depList {
		imports = append(imports, pkg.Import{
			Target: dep.Target,
			Resolved: pkg.ID{
				Type:     pkg.Python,
				Name:     dep.Package,
				Revision: dep.Resolved,
			},
		})
	}
	return imports
}

func getTransitiveDeps(directDeps []dependency) map[pkg.ID]pkg.Package {
	graph := make(map[pkg.ID]pkg.Package)
	for _, dep := range directDeps {
		id := pkg.ID{
			Type:     pkg.Python,
			Name:     dep.Package,
			Revision: dep.Resolved,
		}

		graph[id] = pkg.Package{
			ID:      id,
			Imports: packageImports(dep.Dependencies),
		}

		flattenDeepDependencies(graph, dep)
	}

	return graph
}

func flattenDeepDependencies(graph map[pkg.ID]pkg.Package, transDep dependency) {
	for _, dep := range transDep.Dependencies {
		id := pkg.ID{
			Type:     pkg.Python,
			Name:     dep.Package,
			Revision: dep.Resolved,
		}
		// Don't process duplicate transitive dependencies.
		_, ok := graph[id]
		if ok {
			continue
		}

		graph[id] = pkg.Package{
			ID:      id,
			Imports: packageImports(dep.Dependencies),
		}
		flattenDeepDependencies(graph, dep)
	}
}

func packageImports(packageDeps []dependency) []pkg.Import {
	var imports []pkg.Import
	for _, i := range packageDeps {
		imports = append(imports, pkg.Import{
			Resolved: pkg.ID{
				Type:     pkg.Python,
				Name:     i.Package,
				Revision: i.Resolved,
			},
		})
	}
	return imports
}
