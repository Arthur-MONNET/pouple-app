//go:build !darwin && !linux

package main

func sameAssistantUser(_ int) bool { return false }
