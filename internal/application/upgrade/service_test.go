package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	interactionapp "feidex/internal/application/interaction"
	"feidex/internal/domain/interaction"
	"sync"
	"testing"
	"time"
)

type upgradeRepository struct {
	mu            sync.Mutex
	request       interaction.PendingRequest
	failUpgrading bool
}

func (r *upgradeRepository) Pending(string) *interaction.PendingRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := r.request
	return &copy
}
func (r *upgradeRepository) PendingRequests() []*interaction.PendingRequest {
	return []*interaction.PendingRequest{r.Pending("")}
}
func (r *upgradeRepository) NextLocalID(string) (string, error) { return "op", nil }
func (r *upgradeRepository) SavePending(p *interaction.PendingRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.request = *p
	return nil
}
func (r *upgradeRepository) UpdatePending(_ string, mutate func(*interaction.PendingRequest)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.request
	mutate(&next)
	if r.failUpgrading && next.Status == "upgrading" {
		return errors.New("disk unavailable")
	}
	r.request = next
	return nil
}

type upgradePlatform struct{}

func (upgradePlatform) Inspect(context.Context, bool) (Environment, error) {
	return Environment{GOOS: "linux", Installed: true, Running: true}, nil
}

type upgradeLauncher func(context.Context, LaunchSpec) (string, error)

func (f upgradeLauncher) Launch(ctx context.Context, spec LaunchSpec) (string, error) {
	return f(ctx, spec)
}

type upgradeUnits struct{ status *UnitStatus }

func (u upgradeUnits) Query(context.Context, string) (*UnitStatus, error) { return u.status, nil }
func (upgradeUnits) Cleanup(context.Context, string) error                { return nil }
func upgradeFixture(t *testing.T) (*upgradeRepository, Service) {
	t.Helper()
	encoded, err := json.Marshal(Payload{BinaryPath: "/feidex", TargetVersion: "v2"})
	if err != nil {
		t.Fatal(err)
	}
	repo := &upgradeRepository{request: interaction.PendingRequest{ID: "op", Kind: PendingKind, OwnerUserID: "owner", Status: "pending", PayloadJSON: string(encoded), ExpiresAt: time.Now().Add(time.Hour).Unix()}}
	return repo, Service{Forms: &interactionapp.FormService{Repository: repo}, Platform: upgradePlatform{}}
}

func TestLaunchAndPollCannotPrematurelyResolveOrLaunchTwice(t *testing.T) {
	repo, service := upgradeFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	service.Launcher = upgradeLauncher(func(_ context.Context, spec LaunchSpec) (string, error) {
		close(entered)
		<-release
		return spec.UnitName, nil
	})
	done := make(chan error, 1)
	go func() { _, _, err := service.Confirm(context.Background(), "op", "owner", "card"); done <- err }()
	<-entered
	pending := repo.Pending("op")
	if pending.Status != "launching" {
		t.Fatalf("launch phase = %s", pending.Status)
	}
	poller := Poller{Repository: repo, Units: upgradeUnits{}}
	if out, err := poller.Check(context.Background(), pending); err != nil || out != nil {
		t.Fatalf("launch was resolved before unit appeared: %+v, %v", out, err)
	}
	if _, _, err := service.Confirm(context.Background(), "op", "owner", "card"); err == nil {
		t.Error("duplicate confirm admitted")
	}
	if _, err := service.Cancel("op", "owner"); err == nil {
		t.Error("cancel stole launch ownership")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if pending := repo.Pending("op"); pending.Status != "upgrading" {
		t.Fatalf("after launch: %+v", pending)
	}
}

func TestPollRecoversLaunchedUnitWhenPhaseSaveFails(t *testing.T) {
	repo, service := upgradeFixture(t)
	repo.failUpgrading = true
	service.Launcher = upgradeLauncher(func(_ context.Context, spec LaunchSpec) (string, error) { return spec.UnitName, nil })
	if _, _, err := service.Confirm(context.Background(), "op", "owner", "card"); err == nil {
		t.Fatal("expected persistence failure")
	}
	pending := repo.Pending("op")
	if pending.Status != "launching" {
		t.Fatalf("operation identity lost: %+v", pending)
	}
	poller := Poller{Repository: repo, Units: upgradeUnits{status: &UnitStatus{ActiveState: "inactive", Result: "success"}}}
	out, err := poller.Check(context.Background(), pending)
	if err != nil || out == nil || !out.Success {
		t.Fatalf("recovery = %+v, %v", out, err)
	}
	if out, err := poller.Check(context.Background(), pending); err != nil || out != nil {
		t.Fatalf("duplicate terminal publication: %+v, %v", out, err)
	}
}

func TestLateLaunchFailureCannotReopenResolvedOperation(t *testing.T) {
	repo, service := upgradeFixture(t)
	service.Launcher = upgradeLauncher(func(_ context.Context, spec LaunchSpec) (string, error) {
		poller := Poller{Repository: repo, Units: upgradeUnits{status: &UnitStatus{ActiveState: "inactive", Result: "success"}}}
		if out, err := poller.Check(context.Background(), repo.Pending("op")); err != nil || out == nil {
			t.Fatalf("reconcile = %+v, %v", out, err)
		}
		return "", errors.New("lost launch acknowledgement")
	})
	_, _, _ = service.Confirm(context.Background(), "op", "owner", "card")
	if pending := repo.Pending("op"); pending.Status != "resolved" {
		t.Fatalf("late launch result reopened operation: %+v", pending)
	}
}
