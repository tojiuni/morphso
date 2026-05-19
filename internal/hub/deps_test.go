package hub_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso/internal/hub"
)

// helpers

func makeDep(slug, minVersion string, memGB, diskGB float64) hub.DependencyInfo {
	return hub.DependencyInfo{
		Package:    hub.Package{Slug: slug, Name: slug},
		MinVersion: minVersion,
		ResourceRequirements: hub.ResourceRequirements{
			MinMemoryGB: memGB,
			MinDiskGB:   diskGB,
		},
	}
}

func makeRecord(slug, version, status string, at time.Time) hub.InstallRecord {
	return hub.InstallRecord{
		PackageSlug: slug,
		Version:     version,
		Status:      status,
		InstalledAt: at,
	}
}

// --- BuildDependencyPlan tests ---

func TestBuildDependencyPlan_NoHistory(t *testing.T) {
	deps := []hub.DependencyInfo{
		makeDep("postgresql", "15.0", 1, 5),
		makeDep("redis", "7.0", 0.5, 2),
	}
	plan := hub.BuildDependencyPlan(deps, nil)
	require.Len(t, plan, 2)
	assert.Equal(t, hub.DepInstall, plan[0].Action)
	assert.Equal(t, hub.DepInstall, plan[1].Action)
	assert.Empty(t, plan[0].InstalledVersion)
	assert.Empty(t, plan[1].InstalledVersion)
}

func TestBuildDependencyPlan_AlreadyInstalled_Exact(t *testing.T) {
	deps := []hub.DependencyInfo{makeDep("postgresql", "15.0", 1, 5)}
	history := []hub.InstallRecord{
		makeRecord("postgresql", "15.0", "success", time.Now()),
	}
	plan := hub.BuildDependencyPlan(deps, history)
	require.Len(t, plan, 1)
	assert.Equal(t, hub.DepSkip, plan[0].Action)
}

func TestBuildDependencyPlan_NeedsUpdate(t *testing.T) {
	deps := []hub.DependencyInfo{makeDep("postgresql", "15.0", 1, 5)}
	history := []hub.InstallRecord{
		makeRecord("postgresql", "14.5", "success", time.Now()),
	}
	plan := hub.BuildDependencyPlan(deps, history)
	require.Len(t, plan, 1)
	assert.Equal(t, hub.DepUpdate, plan[0].Action)
	assert.Equal(t, "14.5", plan[0].InstalledVersion)
}

func TestBuildDependencyPlan_NoMinVersion(t *testing.T) {
	deps := []hub.DependencyInfo{makeDep("redis", "", 0.5, 2)}
	history := []hub.InstallRecord{
		makeRecord("redis", "6.0", "success", time.Now()),
	}
	plan := hub.BuildDependencyPlan(deps, history)
	require.Len(t, plan, 1)
	assert.Equal(t, hub.DepSkip, plan[0].Action)
}

func TestBuildDependencyPlan_FailedRecordIgnored(t *testing.T) {
	deps := []hub.DependencyInfo{makeDep("postgresql", "15.0", 1, 5)}
	history := []hub.InstallRecord{
		makeRecord("postgresql", "15.0", "failed", time.Now()),
	}
	plan := hub.BuildDependencyPlan(deps, history)
	require.Len(t, plan, 1)
	assert.Equal(t, hub.DepInstall, plan[0].Action)
	assert.Empty(t, plan[0].InstalledVersion)
}

func TestBuildDependencyPlan_MultiVersion(t *testing.T) {
	// Two records for the same slug; the newer one (by InstalledAt) should win.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	deps := []hub.DependencyInfo{makeDep("postgresql", "15.0", 1, 5)}
	history := []hub.InstallRecord{
		makeRecord("postgresql", "13.0", "success", base),
		makeRecord("postgresql", "16.0", "success", base.Add(time.Hour)), // newer
	}
	plan := hub.BuildDependencyPlan(deps, history)
	require.Len(t, plan, 1)
	// 16.0 >= 15.0 → DepSkip
	assert.Equal(t, hub.DepSkip, plan[0].Action)
}

func TestBuildDependencyPlan_MultiVersion_OlderWins_Update(t *testing.T) {
	// Older record has newer version but the MOST RECENT by time should be used.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	deps := []hub.DependencyInfo{makeDep("postgresql", "15.0", 1, 5)}
	history := []hub.InstallRecord{
		makeRecord("postgresql", "16.0", "success", base),             // older time, newer version
		makeRecord("postgresql", "14.0", "success", base.Add(time.Hour)), // newer time, older version
	}
	plan := hub.BuildDependencyPlan(deps, history)
	require.Len(t, plan, 1)
	// most recent InstalledAt = base+1h → version 14.0 < 15.0 → DepUpdate
	assert.Equal(t, hub.DepUpdate, plan[0].Action)
	assert.Equal(t, "14.0", plan[0].InstalledVersion)
}

// --- CheckResources tests ---

func TestCheckResources_AllFit(t *testing.T) {
	mainDep := makeDep("myapp", "1.0", 1.0, 2.0)
	plan := []hub.DepPlanItem{
		{Dep: makeDep("dep-a", "1.0", 0.5, 1.0), Action: hub.DepInstall},
		{Dep: makeDep("dep-b", "1.0", 0.5, 1.0), Action: hub.DepSkip},
	}
	// total install mem = 0.5 + 1.0 (main) = 1.5, disk = 1.0 + 2.0 = 3.0
	// DepSkip dep-b (0.5 mem, 1.0 disk) not counted
	warnings := hub.CheckResources(plan, mainDep, 4.0, 8.0)
	assert.Nil(t, warnings)
}

func TestCheckResources_RAMShort(t *testing.T) {
	mainDep := makeDep("myapp", "1.0", 2.0, 1.0)
	plan := []hub.DepPlanItem{
		{Dep: makeDep("dep-a", "1.0", 2.0, 1.0), Action: hub.DepInstall},
	}
	// total mem = 2.0 + 2.0 = 4.0, need > have (3.0)
	warnings := hub.CheckResources(plan, mainDep, 3.0, 100.0)
	require.Len(t, warnings, 1)
	assert.Equal(t, "RAM", warnings[0].Kind)
	assert.InDelta(t, 4.0, warnings[0].NeedGB, 0.001)
	assert.InDelta(t, 3.0, warnings[0].HaveGB, 0.001)
	assert.InDelta(t, 1.0, warnings[0].ShortByGB, 0.001)
}

func TestCheckResources_SkipNotCounted(t *testing.T) {
	mainDep := makeDep("myapp", "1.0", 0.5, 0.5)
	plan := []hub.DepPlanItem{
		// large skip items should NOT be counted
		{Dep: makeDep("dep-skip", "1.0", 100.0, 100.0), Action: hub.DepSkip},
	}
	// only main dep counts: 0.5 mem, 0.5 disk — well within limits
	warnings := hub.CheckResources(plan, mainDep, 2.0, 2.0)
	assert.Nil(t, warnings)
}

func TestCheckResources_BothShort(t *testing.T) {
	mainDep := makeDep("myapp", "1.0", 3.0, 5.0)
	plan := []hub.DepPlanItem{
		{Dep: makeDep("dep-a", "1.0", 2.0, 4.0), Action: hub.DepUpdate},
	}
	// total mem = 5.0 > 4.0; total disk = 9.0 > 8.0
	warnings := hub.CheckResources(plan, mainDep, 4.0, 8.0)
	require.Len(t, warnings, 2)
	kinds := map[string]bool{}
	for _, w := range warnings {
		kinds[w.Kind] = true
	}
	assert.True(t, kinds["RAM"])
	assert.True(t, kinds["Disk"])
}
