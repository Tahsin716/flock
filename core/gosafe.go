package core

import "log"

// GoSafe runs a goroutine that recovers from panic.
// Useful for fire-and-forget tasks outside a Group.
func GoSafe(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("goroutine panic recovered: %v", r)
			}
		}()
		fn()
	}()
}
