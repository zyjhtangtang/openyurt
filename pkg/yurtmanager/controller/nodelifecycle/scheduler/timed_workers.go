/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package scheduler

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

// WorkArgs keeps arguments that will be passed to the function executed by the worker.
type WorkArgs struct {
	NamespacedName types.NamespacedName
}

// KeyFromWorkArgs creates a key for the given `WorkArgs`
func (w *WorkArgs) KeyFromWorkArgs() string {
	return w.NamespacedName.String()
}

// NewWorkArgs is a helper function to create new `WorkArgs`
func NewWorkArgs(name, namespace string) *WorkArgs {
	return &WorkArgs{types.NamespacedName{Namespace: namespace, Name: name}}
}

// TimedWorker is a responsible for executing a function no earlier than at FireAt time.
type TimedWorker struct {
	WorkItem  *WorkArgs
	CreatedAt time.Time
	FireAt    time.Time
	Timer     clock.Timer
}

// createWorker creates a TimedWorker that will execute `f` not earlier than `fireAt`.
func createWorker(ctx context.Context, args *WorkArgs, createdAt time.Time, fireAt time.Time, f func(ctx context.Context, args *WorkArgs) error, clock clock.WithDelayedExecution) *TimedWorker {
	delay := fireAt.Sub(createdAt)
	fWithErrorLogging := func() {
		err := f(ctx, args)
		if err != nil {
			klog.Errorf("NodeLifecycle: timed worker failed, %v", err)
		}
	}
	if delay <= 0 {
		go fWithErrorLogging()
		return nil
	}
	timer := clock.AfterFunc(delay, fWithErrorLogging)
	return &TimedWorker{
		WorkItem:  args,
		CreatedAt: createdAt,
		FireAt:    fireAt,
		Timer:     timer,
	}
}

// Cancel cancels the execution of function by the `TimedWorker`
func (w *TimedWorker) Cancel() {
	if w != nil {
		w.Timer.Stop()
	}
}

// TimedWorkerQueue keeps a set of TimedWorkers that are still wait for execution.
type TimedWorkerQueue struct {
	sync.Mutex
	// map of workers keyed by string returned by 'KeyFromWorkArgs' from the given worker.
	workers  map[string]*TimedWorker
	workFunc func(ctx context.Context, args *WorkArgs) error
	clock    clock.WithDelayedExecution
}

// CreateWorkerQueue creates a new TimedWorkerQueue for workers that will execute
// given function `f`.
func CreateWorkerQueue(f func(ctx context.Context, args *WorkArgs) error) *TimedWorkerQueue {
	return &TimedWorkerQueue{
		workers:  make(map[string]*TimedWorker),
		workFunc: f,
		clock:    clock.RealClock{},
	}
}

func (q *TimedWorkerQueue) getWrappedWorkerFunc(key string) func(ctx context.Context, args *WorkArgs) error {
	return func(ctx context.Context, args *WorkArgs) error {
		err := q.workFunc(ctx, args)
		q.Lock()
		defer q.Unlock()
		if err == nil {
			// To avoid duplicated calls we keep the key in the queue, to prevent
			// subsequent additions.
			q.workers[key] = nil
		} else {
			delete(q.workers, key)
		}
		return err
	}
}

// AddWork adds a work to the WorkerQueue which will be executed not earlier than `fireAt`.
func (q *TimedWorkerQueue) AddWork(ctx context.Context, args *WorkArgs, createdAt time.Time, fireAt time.Time) {
	key := args.KeyFromWorkArgs()
	klog.V(4).Infof("Adding TimedWorkerQueue item=%s at createTime=%v and to be fired at firedTime=%v", key, createdAt, fireAt)

	q.Lock()
	defer q.Unlock()
	if _, exists := q.workers[key]; exists {
		klog.Infof("Trying to add already existing work(%v), skipping", args)
		return
	}
	worker := createWorker(ctx, args, createdAt, fireAt, q.getWrappedWorkerFunc(key), q.clock)
	q.workers[key] = worker
}

// CancelWork removes scheduled function execution from the queue. Returns true if work was cancelled.
func (q *TimedWorkerQueue) CancelWork(key string) bool {
	q.Lock()
	defer q.Unlock()
	worker, found := q.workers[key]
	result := false
	if found {
		klog.V(4).Infof("Cancelling TimedWorkerQueue item=%s, time=%v", key, time.Now())
		if worker != nil {
			result = true
			worker.Cancel()
		}
		delete(q.workers, key)
	}
	return result
}

// GetWorkerUnsafe returns a TimedWorker corresponding to the given key.
// Unsafe method - workers have attached goroutines which can fire after this function is called.
func (q *TimedWorkerQueue) GetWorkerUnsafe(key string) *TimedWorker {
	q.Lock()
	defer q.Unlock()
	return q.workers[key]
}
