package maven

import (
	"context"
	"fmt"
	"github.com/murphysecurity/murphysec/utils"
	"github.com/vifraa/gopom"
	"go.uber.org/zap"
)

func BuildDepTree(ctx context.Context, resolver *PomResolver, coordinate Coordinate) *Dependency {
	analyzer := &depAnalyzer{
		Context:           ctx,
		visitedCoordinate: map[Coordinate]bool{},
		exclusionName:     map[string]int{},
		resolver:          resolver,
		logger:            utils.UseLogger(ctx),
		versionResolved:   map[string]string{},
		//resolved:          map[Coordinate]struct{}{},
	}
	tree := analyzer._tree(coordinate)
	VersionReconciling(ctx, tree)
	return tree
}

type depAnalyzer struct {
	context.Context
	visitedCoordinate map[Coordinate]bool
	exclusionName     map[string]int
	resolver          *PomResolver
	logger            *zap.Logger
	depth             int
	versionResolved   map[string]string
}

func (d *depAnalyzer) shouldSkip(coordinate Coordinate) bool {
	return d.visitedCoordinate[coordinate] || d.exclusionName[coordinate.GroupId+":"+coordinate.ArtifactId] > 0
}

func (d *depAnalyzer) visitEnter(coordinate Coordinate) {
	if d.visitedCoordinate[coordinate] {
		panic("revisited")
	}
	d.visitedCoordinate[coordinate] = true
}

func (d *depAnalyzer) visitExit(coordinate Coordinate) {
	delete(d.visitedCoordinate, coordinate)
}

func (d *depAnalyzer) addExclusionSlice(exclusions []gopom.Exclusion) {
	for _, exclusion := range exclusions {
		d.addExclusion(exclusion)
	}
}

func (d *depAnalyzer) removeExclusionSlice(exclusions []gopom.Exclusion) {
	for _, exclusion := range exclusions {
		d.removeExclusion(exclusion)
	}
}

func (d *depAnalyzer) addExclusion(exclusion gopom.Exclusion) {
	k := exclusion.GroupID + ":" + exclusion.ArtifactID
	d.exclusionName[k] = d.exclusionName[k] + 1
}

func (d *depAnalyzer) removeExclusion(exclusion gopom.Exclusion) {
	k := exclusion.GroupID + ":" + exclusion.ArtifactID
	if d.exclusionName[k] > 0 {
		d.exclusionName[k] = d.exclusionName[k] - 1
	}
	if d.exclusionName[k] < 0 {
		panic("< 0")
	}
}

func (d *depAnalyzer) _tree(coordinate Coordinate) *Dependency {
	d.depth++
	defer func() {
		d.depth--
	}()
	var logger = d.logger
	if d.shouldSkip(coordinate) {
		return nil
	}
	if v := d.versionResolved[coordinate.GroupId+coordinate.ArtifactId]; v != "" {
		return nil
	}
	pom, e := d.resolver.ResolvePom(d, coordinate)
	if e != nil {
		logger.Warn(fmt.Sprintf("Resolve %s failed", coordinate), zap.Error(e))
		return nil
	}
	current := &Dependency{
		Coordinate: pom.Coordinate,
		Children:   []Dependency{},
	}
	d.versionResolved[coordinate.GroupId+coordinate.ArtifactId] = pom.Coordinate.Version
	d.visitEnter(pom.Coordinate)
	defer d.visitExit(pom.Coordinate)
	for _, dependency := range pom.ListDeps() {
		if !(dependency.Scope == "" || dependency.Scope == "compile" || dependency.Scope == "runtime") || dependency.Optional == "true" {
			continue
		}
		depCoor := Coordinate{
			GroupId:    dependency.GroupID,
			ArtifactId: dependency.ArtifactID,
			Version:    dependency.Version,
		}
		if !depCoor.Complete() {
			logger.Warn("Incomplete coordinate, skip", zap.Any("coordinate", depCoor), zap.Any("in", coordinate))
			continue
		}
		d.addExclusionSlice(dependency.Exclusions)
		r := d._tree(depCoor)
		d.removeExclusionSlice(dependency.Exclusions)
		if r != nil {
			current.Children = append(current.Children, *r)
		}
	}
	return current
}
