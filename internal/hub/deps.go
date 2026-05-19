package hub

import (
	"strconv"
	"strings"
)

// DepAction represents what to do with a dependency.
type DepAction int

const (
	DepSkip    DepAction = iota // already installed at sufficient version
	DepUpdate                   // installed but version too old
	DepInstall                  // not installed
)

// DepPlanItem is one line in the dependency install plan.
type DepPlanItem struct {
	Dep              DependencyInfo
	Action           DepAction
	InstalledVersion string // current installed version (empty if DepInstall)
}

// ResourceWarning describes a resource shortfall.
type ResourceWarning struct {
	Kind      string  // "RAM" or "Disk"
	NeedGB    float64
	HaveGB    float64
	ShortByGB float64
}

// CompareVersion compares two semver-ish version strings part by part numerically.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
// Leading "v" prefix is stripped. Non-numeric parts fall back to string comparison.
func CompareVersion(a, b string) int {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")

	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		var aPart, bPart string
		if i < len(aParts) {
			aPart = aParts[i]
		}
		if i < len(bParts) {
			bPart = bParts[i]
		}

		// Missing parts (empty string from a shorter version) are treated as numeric 0.
		aNum, aErr := strconv.Atoi(aPart)
		bNum, bErr := strconv.Atoi(bPart)

		aMissing := aPart == ""
		bMissing := bPart == ""

		if (aErr == nil || aMissing) && (bErr == nil || bMissing) {
			// both parts are numeric (or absent — treated as 0)
			if aMissing {
				aNum = 0
			}
			if bMissing {
				bNum = 0
			}
			if aNum < bNum {
				return -1
			}
			if aNum > bNum {
				return 1
			}
		} else {
			// at least one part is non-numeric and non-empty: fall back to string comparison
			if aPart < bPart {
				return -1
			}
			if aPart > bPart {
				return 1
			}
		}
	}
	return 0
}

// BuildDependencyPlan compares the hub dependency list against the user's
// install history and produces an ordered plan.
func BuildDependencyPlan(deps []DependencyInfo, history []InstallRecord) []DepPlanItem {
	plan := make([]DepPlanItem, 0, len(deps))

	for _, dep := range deps {
		// Find the most recent non-failed install record for this package slug.
		var best *InstallRecord
		for i := range history {
			rec := &history[i]
			if rec.PackageSlug != dep.Package.Slug {
				continue
			}
			// Empty status means success (backward compat); "failed" is excluded.
			if rec.Status == "failed" {
				continue
			}
			if best == nil || rec.InstalledAt.After(best.InstalledAt) {
				best = rec
			}
		}

		if best == nil {
			// Not installed.
			plan = append(plan, DepPlanItem{Dep: dep, Action: DepInstall})
			continue
		}

		// Installed — check version requirement.
		if dep.MinVersion == "" {
			// Any version is fine.
			plan = append(plan, DepPlanItem{Dep: dep, Action: DepSkip, InstalledVersion: best.Version})
			continue
		}

		if CompareVersion(best.Version, dep.MinVersion) >= 0 {
			plan = append(plan, DepPlanItem{Dep: dep, Action: DepSkip, InstalledVersion: best.Version})
		} else {
			plan = append(plan, DepPlanItem{Dep: dep, Action: DepUpdate, InstalledVersion: best.Version})
		}
	}

	return plan
}

// CheckResources computes total resource requirements for DepInstall and DepUpdate
// items and compares against available resources.
// Returns nil if all requirements are met.
func CheckResources(plan []DepPlanItem, mainDep DependencyInfo, memFreeGB, diskFreeGB float64) []ResourceWarning {
	totalMemGB := mainDep.ResourceRequirements.MinMemoryGB
	totalDiskGB := mainDep.ResourceRequirements.MinDiskGB

	for _, item := range plan {
		if item.Action == DepSkip {
			continue
		}
		totalMemGB += item.Dep.ResourceRequirements.MinMemoryGB
		totalDiskGB += item.Dep.ResourceRequirements.MinDiskGB
	}

	var warnings []ResourceWarning

	if totalMemGB > memFreeGB {
		warnings = append(warnings, ResourceWarning{
			Kind:      "RAM",
			NeedGB:    totalMemGB,
			HaveGB:    memFreeGB,
			ShortByGB: totalMemGB - memFreeGB,
		})
	}

	if totalDiskGB > diskFreeGB {
		warnings = append(warnings, ResourceWarning{
			Kind:      "Disk",
			NeedGB:    totalDiskGB,
			HaveGB:    diskFreeGB,
			ShortByGB: totalDiskGB - diskFreeGB,
		})
	}

	if len(warnings) == 0 {
		return nil
	}
	return warnings
}
