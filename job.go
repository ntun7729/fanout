package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// JobStep is one target in a job. The UI shows each step separately so users
// can see exactly which target is stalled or failed.
type JobStep struct {
	Label  string `json:"label"`
	Status string `json:"status"` // pending | running | ok | failed
	Detail string `json:"detail"`
}

// Job represents one batch exit-provisioning operation.
type Job struct {
	mu      sync.Mutex
	id      string
	summary string
	status  string
	steps   []*JobStep
	started time.Time
	ended   time.Time
}

// JobView is a read-only snapshot returned to the UI.
type JobView struct {
	ID      string    `json:"id"`
	Summary string    `json:"summary"`
	Status  string    `json:"status"` // running | done | failed
	Steps   []JobStep `json:"steps"`
	Started time.Time `json:"started"`
	Done    int       `json:"done"`
	Total   int       `json:"total"`
}

func (j *Job) ID() string { return j.id }

// Set updates the state of one step.
func (j *Job) Set(i int, status, detail string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if i < 0 || i >= len(j.steps) {
		return
	}
	j.steps[i].Status = status
	j.steps[i].Detail = detail
}

// Finish completes the job. If any step failed, the entire job is marked failed.
func (j *Job) Finish() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status = "done"
	for _, s := range j.steps {
		if s.Status == "failed" {
			j.status = "failed"
			break
		}
	}
	j.ended = time.Now()
}

func (j *Job) View() JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	v := JobView{
		ID: j.id, Summary: j.summary, Status: j.status,
		Started: j.started, Total: len(j.steps),
		Steps: make([]JobStep, 0, len(j.steps)),
	}
	for _, s := range j.steps {
		v.Steps = append(v.Steps, *s)
		if s.Status == "ok" || s.Status == "failed" {
			v.Done++
		}
	}
	return v
}

// JobStore keeps recent jobs. Jobs are transient and do not need to survive a process restart.
type JobStore struct {
	mu   sync.Mutex
	jobs []*Job
}

// keepJobs limits retained history so long-running processes do not grow indefinitely.
const keepJobs = 8

func (s *JobStore) New(summary string, labels []string) *Job {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	j := &Job{
		id: hex.EncodeToString(buf), summary: summary,
		status: "running", started: time.Now(),
	}
	for _, l := range labels {
		j.steps = append(j.steps, &JobStep{Label: l, Status: "pending"})
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, j)
	if len(s.jobs) > keepJobs {
		s.jobs = s.jobs[len(s.jobs)-keepJobs:]
	}
	return j
}

// Views returns recent jobs with the newest first.
func (s *JobStore) Views() []JobView {
	s.mu.Lock()
	jobs := make([]*Job, len(s.jobs))
	copy(jobs, s.jobs)
	s.mu.Unlock()

	out := make([]JobView, 0, len(jobs))
	for i := len(jobs) - 1; i >= 0; i-- {
		out = append(out, jobs[i].View())
	}
	return out
}

// Dismiss removes a completed job when the user closes it in the UI.
func (s *JobStore) Dismiss(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.jobs[:0]
	for _, j := range s.jobs {
		if j.id != id {
			kept = append(kept, j)
		}
	}
	s.jobs = kept
}
