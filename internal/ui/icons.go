package ui

import "fyne.io/fyne/v2"

var (
	lockIconResource = fyne.NewStaticResource("lock.svg", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path d="M17 9h-1V7a4 4 0 0 0-8 0v2H7a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-9a2 2 0 0 0-2-2zm-7-2a2 2 0 0 1 4 0v2h-4V7zm7 13H7v-9h10v9z"/>
</svg>`))
	unlockIconResource = fyne.NewStaticResource("unlock.svg", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path d="M17 9h-7V7a2 2 0 0 1 4 0h2a4 4 0 0 0-8 0v2H7a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-9a2 2 0 0 0-2-2zm0 11H7v-9h10v9z"/>
</svg>`))
)

func lockIcon() fyne.Resource {
	return lockIconResource
}

func unlockIcon() fyne.Resource {
	return unlockIconResource
}
