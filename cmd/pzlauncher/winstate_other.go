//go:build !windows

package main

import "fyne.io/fyne/v2"

func nativeSave(w fyne.Window, s *winState) {}

func (u *ui) nativeRestore() {}
