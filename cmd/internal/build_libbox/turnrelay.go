//go:build with_turnrelay

package main

func init() {
	sharedTags = append(sharedTags, "with_turnrelay")
}
