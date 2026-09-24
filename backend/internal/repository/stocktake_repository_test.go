package repository

import (
	"context"
	"database/sql/driver"
	"strings"
	"sync"
	"testing"
	"time"

	gosqlite "github.com/glebarez/go-sqlite"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"biosample-cold-custody-tracking/backend/internal/constants"
	"biosample-cold-custody-tracking/backend/internal/model"
)

var registerGreatestOnce sync.Once

func init() {
	// PostgreSQL ships GREATEST(); the production SQL relies on it (see custody
	// transfers). Register an equivalent scalar function on the pure-Go SQLite
	// test driver so the repository tests run without CGO.
	registerGreatestOnce.Do(func() {
		gosqlite.MustRegisterScalarFunction("greatest", 2, func(_ *gosqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			toi := func(v driver.Value) int64 {
				if n, ok := v.(int64); ok {
					return n
				}
				return 0
			}
			a, b := toi(args[0]), toi(args[1])
			if a > b {
				return a, nil
			}
			return b, nil
		})
	})
}

func newStocktakeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.StorageContainer{}, &model.Specimen{}, &model.Stocktake{}, &model.StocktakeItem{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Each test gets a clean schema.
	if err := db.Exec("DELETE FROM stocktake_items").Error; err != nil {
		t.Fatal(err)
	}
	db.Exec("DELETE FROM stocktakes")
	db.Exec("DELETE FROM specimens")
	db.Exec("DELETE FROM storage_containers")
	return db
}

func seedStocktakeScenario(t *testing.T, db *gorm.DB) (model.StorageContainer, model.StorageContainer, []model.Specimen) {
	t.Helper()
	source := model.StorageContainer{
		Code: "FZ-80-SRC", Name: "源冻存柜", ContainerType: "freezer", TemperatureZone: "minus80",
		Location: "B 区", Capacity: 10, Occupied: 3, Status: "available", Active: true,
	}
	target := model.StorageContainer{
		Code: "FZ-80-TGT", Name: "目标冻存柜", ContainerType: "freezer", TemperatureZone: "minus80",
		Location: "B 区", Capacity: 10, Occupied: 0, Status: "available", Active: true,
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	positions := []string{"P1", "P2", "P3"}
	specimens := make([]model.Specimen, 0, 3)
	for _, pos := range positions {
		containerID := source.ID
		s := model.Specimen{
			AccessionNo:        "BIO-ST-" + pos,
			SampleType:         "血浆",
			SubjectCode:        "SUBJ-" + pos,
			ProtocolCode:       "PROTO-ST-1",
			State:              constants.SpecimenStateStored,
			StorageContainerID: &containerID,
			Position:           pos,
			VolumeML:           1,
			CurrentCustodian:   "保管员",
			ReceivedAt:         time.Now().Add(-time.Hour),
		}
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
		specimens = append(specimens, s)
	}
	return source, target, specimens
}

func markItemDirect(t *testing.T, db *gorm.DB, repo StocktakeRepository, taskID uint, itemID uint, result constants.StocktakeResult, targetContainerID *uint, newPosition, note string) {
	t.Helper()
	mark := StocktakeMark{
		Result: result, Note: note, NewContainerID: targetContainerID, NewPosition: newPosition,
		MarkedByID: 1, MarkedByName: "保管员", MarkedAt: time.Now().UTC(),
	}
	if _, err := repo.MarkItem(context.Background(), taskID, itemID, mark); err != nil {
		t.Fatalf("mark item %d as %s: %v", itemID, result, err)
	}
}

func TestStocktakeCloseAppliesRelocationAndOccupancy(t *testing.T) {
	db := newStocktakeTestDB(t)
	repo := NewStocktakeRepository(db)
	source, target, specimens := seedStocktakeScenario(t, db)

	task := &model.Stocktake{
		StocktakeNo: "ST-CLOSE-001", TaskMonth: "2026-09", ContainerID: source.ID,
		State: constants.StocktakeStateInProgress, StartedByID: 1, StartedByName: "保管员", StartedAt: time.Now().UTC(),
	}
	if err := repo.CreateWithSnapshot(context.Background(), task); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	created, err := repo.Find(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if created.TotalItems != 3 {
		t.Fatalf("snapshot must freeze 3 stored specimens, got %d", created.TotalItems)
	}
	items := created.Items
	bySpecimen := map[uint]model.StocktakeItem{}
	for _, item := range items {
		bySpecimen[item.SpecimenID] = item
	}
	// P1 in place, P2 missing, P3 mislocated into target container.
	markItemDirect(t, db, repo, task.ID, bySpecimen[specimens[0].ID].ID, constants.StocktakeResultInPlace, nil, "", "")
	p2 := bySpecimen[specimens[1].ID]
	markItemDirect(t, db, repo, task.ID, p2.ID, constants.StocktakeResultMissing, nil, "", "管架空缺")
	p3 := bySpecimen[specimens[2].ID]
	markItemDirect(t, db, repo, task.ID, p3.ID, constants.StocktakeResultMislocated, &target.ID, "Q1", "")

	outcome, err := repo.Close(context.Background(), task.ID, StocktakeResolution{ClosedByID: 1, ClosedByName: "保管员", ClosedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("close stocktake: %v", err)
	}
	if outcome.Stocktake.State != constants.StocktakeStateClosed {
		t.Fatalf("expected closed, got %s", outcome.Stocktake.State)
	}
	if len(outcome.Changes) != 2 {
		t.Fatalf("expected 2 specimen changes (missing + moved), got %d", len(outcome.Changes))
	}
	var updatedSource, updatedTarget model.StorageContainer
	db.First(&updatedSource, source.ID)
	db.First(&updatedTarget, target.ID)
	// Source: -1 missing -1 moved = 1 occupied. Target: +1 moved = 1 occupied.
	if updatedSource.Occupied != 1 {
		t.Fatalf("source occupied = %d, want 1", updatedSource.Occupied)
	}
	if updatedTarget.Occupied != 1 {
		t.Fatalf("target occupied = %d, want 1", updatedTarget.Occupied)
	}
	var moved model.Specimen
	db.First(&moved, specimens[2].ID)
	if moved.StorageContainerID == nil || *moved.StorageContainerID != target.ID || moved.Position != "Q1" {
		t.Fatal("mislocated specimen must be relocated to the target container and position")
	}
	var missing model.Specimen
	db.First(&missing, specimens[1].ID)
	if missing.State != constants.SpecimenStateDisposed || missing.StorageContainerID != nil || missing.Position != "" {
		t.Fatal("missing specimen must be disposed and removed from the container")
	}
	if !strings.Contains(missing.Notes, "盘点缺失: 管架空缺") {
		t.Fatalf("missing reason must be recorded on the specimen, got notes %q", missing.Notes)
	}
}

func TestStocktakeCloseRejectsCloseBeforeAllProcessed(t *testing.T) {
	db := newStocktakeTestDB(t)
	repo := NewStocktakeRepository(db)
	source, _, specimens := seedStocktakeScenario(t, db)
	task := &model.Stocktake{
		StocktakeNo: "ST-PENDING-001", TaskMonth: "2026-09", ContainerID: source.ID,
		State: constants.StocktakeStateInProgress, StartedByID: 1, StartedByName: "保管员", StartedAt: time.Now().UTC(),
	}
	if err := repo.CreateWithSnapshot(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	created, _ := repo.Find(context.Background(), task.ID)
	first := created.Items[0]
	markItemDirect(t, db, repo, task.ID, first.ID, constants.StocktakeResultInPlace, nil, "", "")
	_, err := repo.Close(context.Background(), task.ID, StocktakeResolution{ClosedByID: 1, ClosedByName: "保管员", ClosedAt: time.Now().UTC()})
	if err != ErrStocktakeNotComplete {
		t.Fatalf("expected ErrStocktakeNotComplete, got %v", err)
	}
	_ = specimens
}

func TestStocktakeCloseDetectsHandoverDrift(t *testing.T) {
	db := newStocktakeTestDB(t)
	repo := NewStocktakeRepository(db)
	source, target, specimens := seedStocktakeScenario(t, db)
	task := &model.Stocktake{
		StocktakeNo: "ST-DRIFT-001", TaskMonth: "2026-09", ContainerID: source.ID,
		State: constants.StocktakeStateInProgress, StartedByID: 1, StartedByName: "保管员", StartedAt: time.Now().UTC(),
	}
	if err := repo.CreateWithSnapshot(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	created, _ := repo.Find(context.Background(), task.ID)
	bySpecimen := map[uint]model.StocktakeItem{}
	for _, item := range created.Items {
		bySpecimen[item.SpecimenID] = item
	}
	markItemDirect(t, db, repo, task.ID, bySpecimen[specimens[0].ID].ID, constants.StocktakeResultInPlace, nil, "", "")
	markItemDirect(t, db, repo, task.ID, bySpecimen[specimens[1].ID].ID, constants.StocktakeResultInPlace, nil, "", "")
	markItemDirect(t, db, repo, task.ID, bySpecimen[specimens[2].ID].ID, constants.StocktakeResultInPlace, nil, "", "")

	// Simulate a handover after the snapshot: specimen moved to another container.
	if err := db.Model(&model.Specimen{}).Where("id = ?", specimens[0].ID).
		Updates(map[string]any{"storage_container_id": target.ID, "position": "X9"}).Error; err != nil {
		t.Fatal(err)
	}
	_, err := repo.Close(context.Background(), task.ID, StocktakeResolution{ClosedByID: 1, ClosedByName: "保管员", ClosedAt: time.Now().UTC()})
	var drift *StocktakeDriftError
	if err == nil {
		t.Fatal("close must fail when a specimen drifted after the stocktake started")
	}
	if !asStocktakeDrift(err, &drift) {
		t.Fatalf("expected drift error, got %T %v", err, err)
	}
	if len(drift.Accessions) != 1 || drift.Accessions[0] != specimens[0].AccessionNo {
		t.Fatalf("drift must name the handed-over specimen, got %v", drift.Accessions)
	}
}

func TestStocktakeRejectsDuplicateNewPosition(t *testing.T) {
	db := newStocktakeTestDB(t)
	repo := NewStocktakeRepository(db)
	source, target, specimens := seedStocktakeScenario(t, db)
	task := &model.Stocktake{
		StocktakeNo: "ST-DUP-001", TaskMonth: "2026-09", ContainerID: source.ID,
		State: constants.StocktakeStateInProgress, StartedByID: 1, StartedByName: "保管员", StartedAt: time.Now().UTC(),
	}
	if err := repo.CreateWithSnapshot(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	created, _ := repo.Find(context.Background(), task.ID)
	bySpecimen := map[uint]model.StocktakeItem{}
	for _, item := range created.Items {
		bySpecimen[item.SpecimenID] = item
	}
	markItemDirect(t, db, repo, task.ID, bySpecimen[specimens[0].ID].ID, constants.StocktakeResultMislocated, &target.ID, "SAME", "")
	markItemDirect(t, db, repo, task.ID, bySpecimen[specimens[1].ID].ID, constants.StocktakeResultMislocated, &target.ID, "SAME", "")
	markItemDirect(t, db, repo, task.ID, bySpecimen[specimens[2].ID].ID, constants.StocktakeResultInPlace, nil, "", "")
	_, err := repo.Close(context.Background(), task.ID, StocktakeResolution{ClosedByID: 1, ClosedByName: "保管员", ClosedAt: time.Now().UTC()})
	if err != ErrStocktakeTargetDup {
		t.Fatalf("expected ErrStocktakeTargetDup, got %v", err)
	}
}

func TestStocktakeRejectsBusyOrEmptyContainer(t *testing.T) {
	db := newStocktakeTestDB(t)
	repo := NewStocktakeRepository(db)
	source, _, _ := seedStocktakeScenario(t, db)
	first := &model.Stocktake{
		StocktakeNo: "ST-BUSY-001", TaskMonth: "2026-09", ContainerID: source.ID,
		State: constants.StocktakeStateInProgress, StartedByID: 1, StartedByName: "保管员", StartedAt: time.Now().UTC(),
	}
	if err := repo.CreateWithSnapshot(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := &model.Stocktake{
		StocktakeNo: "ST-BUSY-002", TaskMonth: "2026-09", ContainerID: source.ID,
		State: constants.StocktakeStateInProgress, StartedByID: 1, StartedByName: "保管员", StartedAt: time.Now().UTC(),
	}
	if err := repo.CreateWithSnapshot(context.Background(), second); err != ErrStocktakeContainerBusy {
		t.Fatalf("expected ErrStocktakeContainerBusy, got %v", err)
	}
	empty := model.StorageContainer{
		Code: "FZ-EMPTY", Name: "空柜", ContainerType: "freezer", TemperatureZone: "minus80",
		Location: "B 区", Capacity: 5, Occupied: 0, Status: "available", Active: true,
	}
	if err := db.Create(&empty).Error; err != nil {
		t.Fatal(err)
	}
	third := &model.Stocktake{
		StocktakeNo: "ST-EMPTY-001", TaskMonth: "2026-09", ContainerID: empty.ID,
		State: constants.StocktakeStateInProgress, StartedByID: 1, StartedByName: "保管员", StartedAt: time.Now().UTC(),
	}
	if err := repo.CreateWithSnapshot(context.Background(), third); err != ErrStocktakeContainerEmpty {
		t.Fatalf("expected ErrStocktakeContainerEmpty, got %v", err)
	}
}

func asStocktakeDrift(err error, target **StocktakeDriftError) bool {
	for err != nil {
		if d, ok := err.(*StocktakeDriftError); ok {
			*target = d
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
