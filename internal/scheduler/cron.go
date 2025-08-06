/*
Copyright 2025 monshunter.

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
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronScheduler provides basic cron scheduling functionality
type CronScheduler struct{}

// NewCronScheduler creates a new cron scheduler
func NewCronScheduler() *CronScheduler {
	return &CronScheduler{}
}

// IsTimeToRun checks if the current time matches the cron schedule
// Supports basic cron format: "minute hour day month weekday"
// For simplicity, this is a basic implementation. In production, consider using github.com/robfig/cron
func (c *CronScheduler) IsTimeToRun(schedule string) (bool, error) {
	if schedule == "" {
		return false, fmt.Errorf("empty schedule")
	}

	// Parse schedule (minute hour day month weekday)
	parts := strings.Fields(schedule)
	if len(parts) != 5 {
		return false, fmt.Errorf("invalid cron format, expected 5 fields: %s", schedule)
	}

	now := time.Now()

	// Check minute (0-59)
	if !c.matchesField(parts[0], now.Minute(), 0, 59) {
		return false, nil
	}

	// Check hour (0-23)
	if !c.matchesField(parts[1], now.Hour(), 0, 23) {
		return false, nil
	}

	// Check day (1-31)
	if !c.matchesField(parts[2], now.Day(), 1, 31) {
		return false, nil
	}

	// Check month (1-12)
	if !c.matchesField(parts[3], int(now.Month()), 1, 12) {
		return false, nil
	}

	// Check weekday (0-6, Sunday = 0)
	if !c.matchesField(parts[4], int(now.Weekday()), 0, 6) {
		return false, nil
	}

	return true, nil
}

// NextRunTime calculates the next time the schedule should run
// This is a simplified implementation
func (c *CronScheduler) NextRunTime(schedule string) (time.Time, error) {
	if schedule == "" {
		return time.Time{}, fmt.Errorf("empty schedule")
	}

	// For simplicity, just add 1 minute to current time
	// In a real implementation, this would calculate based on the cron expression
	return time.Now().Add(time.Minute), nil
}

// matchesField checks if a time field matches a cron field specification
func (c *CronScheduler) matchesField(field string, value, min, max int) bool {
	// Handle wildcard
	if field == "*" {
		return true
	}

	// Handle ranges (e.g., "1-5")
	if strings.Contains(field, "-") {
		parts := strings.Split(field, "-")
		if len(parts) == 2 {
			start, err1 := strconv.Atoi(parts[0])
			end, err2 := strconv.Atoi(parts[1])
			if err1 == nil && err2 == nil {
				return value >= start && value <= end
			}
		}
		return false
	}

	// Handle step values (e.g., "*/5")
	if strings.Contains(field, "/") {
		parts := strings.Split(field, "/")
		if len(parts) == 2 && parts[0] == "*" {
			step, err := strconv.Atoi(parts[1])
			if err == nil && step > 0 {
				return value%step == 0
			}
		}
		return false
	}

	// Handle comma-separated values (e.g., "1,3,5")
	if strings.Contains(field, ",") {
		values := strings.Split(field, ",")
		for _, v := range values {
			if val, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && val == value {
				return true
			}
		}
		return false
	}

	// Handle single value
	if val, err := strconv.Atoi(field); err == nil {
		return val == value
	}

	return false
}

// ValidateSchedule validates a cron schedule expression
func (c *CronScheduler) ValidateSchedule(schedule string) error {
	if schedule == "" {
		return fmt.Errorf("empty schedule")
	}

	parts := strings.Fields(schedule)
	if len(parts) != 5 {
		return fmt.Errorf("invalid cron format, expected 5 fields: %s", schedule)
	}

	// Validate each field
	fields := []struct {
		name string
		min  int
		max  int
	}{
		{"minute", 0, 59},
		{"hour", 0, 23},
		{"day", 1, 31},
		{"month", 1, 12},
		{"weekday", 0, 6},
	}

	for i, field := range fields {
		if err := c.validateField(parts[i], field.name, field.min, field.max); err != nil {
			return fmt.Errorf("invalid %s field: %w", field.name, err)
		}
	}

	return nil
}

// validateField validates a single cron field
func (c *CronScheduler) validateField(field, name string, min, max int) error {
	// Handle wildcard
	if field == "*" {
		return nil
	}

	// Handle ranges
	if strings.Contains(field, "-") {
		parts := strings.Split(field, "-")
		if len(parts) != 2 {
			return fmt.Errorf("invalid range format: %s", field)
		}
		start, err1 := strconv.Atoi(parts[0])
		end, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return fmt.Errorf("invalid range values: %s", field)
		}
		if start < min || start > max || end < min || end > max || start > end {
			return fmt.Errorf("range out of bounds (%d-%d): %s", min, max, field)
		}
		return nil
	}

	// Handle step values
	if strings.Contains(field, "/") {
		parts := strings.Split(field, "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid step format: %s", field)
		}
		if parts[0] != "*" {
			return fmt.Errorf("step values must start with *: %s", field)
		}
		step, err := strconv.Atoi(parts[1])
		if err != nil || step <= 0 {
			return fmt.Errorf("invalid step value: %s", field)
		}
		return nil
	}

	// Handle comma-separated values
	if strings.Contains(field, ",") {
		values := strings.Split(field, ",")
		for _, v := range values {
			val, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return fmt.Errorf("invalid value: %s", v)
			}
			if val < min || val > max {
				return fmt.Errorf("value out of bounds (%d-%d): %d", min, max, val)
			}
		}
		return nil
	}

	// Handle single value
	val, err := strconv.Atoi(field)
	if err != nil {
		return fmt.Errorf("invalid value: %s", field)
	}
	if val < min || val > max {
		return fmt.Errorf("value out of bounds (%d-%d): %d", min, max, val)
	}

	return nil
}
