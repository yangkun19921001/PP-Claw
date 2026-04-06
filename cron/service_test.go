package cron

import (
	"path/filepath"
	"testing"
)

func TestOnTimerDeletesOneTimeJobWithoutPanicking(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	svc := NewService(storePath, nil)
	svc.SetOnJob(func(job *CronJob) (string, error) {
		return "ok", nil
	})

	now := nowMs()
	svc.store = &CronStore{
		Version: 1,
		Jobs: []CronJob{
			{
				ID:      "job1",
				Name:    "delete-after-run",
				Enabled: true,
				Schedule: CronSchedule{
					Kind: "at",
					AtMs: now - 1000,
				},
				State: CronJobState{
					NextRunAtMs: now - 1000,
				},
				DeleteAfterRun: true,
			},
			{
				ID:      "job2",
				Name:    "keep-running",
				Enabled: true,
				Schedule: CronSchedule{
					Kind:    "every",
					EveryMs: 60_000,
				},
				State: CronJobState{
					NextRunAtMs: now - 1000,
				},
			},
		},
	}

	svc.onTimer()

	if len(svc.store.Jobs) != 1 {
		t.Fatalf("expected 1 job after timer run, got %d", len(svc.store.Jobs))
	}
	if svc.store.Jobs[0].ID != "job2" {
		t.Fatalf("expected remaining job to be job2, got %s", svc.store.Jobs[0].ID)
	}
	if svc.store.Jobs[0].State.LastStatus != "ok" {
		t.Fatalf("expected recurring job to execute successfully, got status %q", svc.store.Jobs[0].State.LastStatus)
	}
	if svc.store.Jobs[0].State.NextRunAtMs <= now {
		t.Fatalf("expected recurring job next run to be updated, got %d", svc.store.Jobs[0].State.NextRunAtMs)
	}
}
