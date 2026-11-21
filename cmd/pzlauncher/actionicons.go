package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Icons for the Workshop item actions (Steam-style vote / favorite / award /
// share / add-to-collection buttons). Original simple shapes drawn for this
// project; themed so they follow the button text colour.

func svgIcon(name, body string) fyne.Resource {
	return theme.NewThemedResource(fyne.NewStaticResource(name+".svg",
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#000000">`+body+`</svg>`)))
}

const thumbPath = `<rect x="2" y="10" width="4" height="11" rx="1"/>` +
	`<path d="M8 10.5 L11.8 3.4 C12.4 2.3 14.3 2.6 14.3 4.2 L13.6 9 H19.8 C21.2 9 22.1 10.3 21.7 11.6 L19.6 19.6 C19.3 20.5 18.5 21 17.6 21 H9 C8.4 21 8 20.6 8 20 Z"/>`

var (
	iconThumbUp   = svgIcon("thumbup", thumbPath)
	iconThumbDown = svgIcon("thumbdown", `<g transform="rotate(180 12 12)">`+thumbPath+`</g>`)
	iconStar      = svgIcon("star", `<path d="M12 2.5 L14.8 8.6 L21.5 9.3 L16.5 13.8 L17.9 20.4 L12 17 L6.1 20.4 L7.5 13.8 L2.5 9.3 L9.2 8.6 Z"/>`)
	iconAward     = svgIcon("award", `<path d="M5.5 2 H9.8 L13 9.2 L10.6 10.6 Z M18.5 2 H14.2 L11 9.2 L13.4 10.6 Z"/>`+
		`<circle cx="12" cy="15.5" r="6.5"/><path d="M12 11.6 L13.2 14.2 L16 14.5 L13.9 16.4 L14.5 19.2 L12 17.8 L9.5 19.2 L10.1 16.4 L8 14.5 L10.8 14.2 Z" fill="#ffffff" fill-opacity="0.45"/>`)
	iconShare = svgIcon("share", `<circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/>`+
		`<path d="M7.6 10.6 L16.2 6.2 L17 7.8 L8.4 12.2 Z M8.4 11.8 L17 16.2 L16.2 17.8 L7.6 13.4 Z"/>`)
	iconAddColl = svgIcon("addcollection", `<path d="M3 6 H5 V19 H18 V21 H4 C3.4 21 3 20.6 3 20 Z"/>`+
		`<path d="M8 2 H20 C21.1 2 22 2.9 22 4 V16 C22 17.1 21.1 18 20 18 H8 C6.9 18 6 17.1 6 16 V4 C6 2.9 6.9 2 8 2 Z M13 5 V9 H9 V11 H13 V15 H15 V11 H19 V9 H15 V5 Z" fill-rule="evenodd"/>`)
)
