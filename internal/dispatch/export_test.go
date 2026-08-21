// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package dispatch

import "time"

// SetHandlerTimeoutForTest shortens the handler timeout and returns a
// function restoring it, so the timeout path can be exercised without
// spending HandlerTimeout of wall clock in CI.
func SetHandlerTimeoutForTest(d time.Duration) func() {
	prev := handlerTimeout
	handlerTimeout = d
	return func() { handlerTimeout = prev }
}
