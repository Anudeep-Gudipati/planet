/*
Copyright 2018 Gravitational, Inc.

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

package utils

import (
	"context"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gravitational/trace"
)

// Retry retries 'times' attempts with retry period 'period' calling function fn
// until it returns nil, or until the context gets cancelled or the retries
// get exceeded the times number of attempts
func Retry(ctx context.Context, times int, period time.Duration, fn func() error) error {
	var err error
	for i := 0; i < times; i += 1 {
		err = fn()
		if err == nil {
			return nil
		}
		log.Debugf("attempt %v, result: %v, retry in %v", i+1, err, period)
		select {
		case <-ctx.Done():
			log.Debugf("context is closing, return")
			return err
		case <-time.After(period):
		}
	}
	return trace.Wrap(err)
}
