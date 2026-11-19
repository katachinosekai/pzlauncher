package main

import (
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

type tabDef struct {
	order int
	title string
	build func(*ui) fyne.CanvasObject
}

var tabRegistry []tabDef

func registerTab(order int, title string, build func(*ui) fyne.CanvasObject) {
	tabRegistry = append(tabRegistry, tabDef{order, title, build})
}

func (u *ui) buildTabs() *container.AppTabs {
	defs := append([]tabDef(nil), tabRegistry...)
	sort.SliceStable(defs, func(i, j int) bool { return defs[i].order < defs[j].order })
	tabs := container.NewAppTabs()
	for _, d := range defs {
		tabs.Append(container.NewTabItem(d.title, d.build(u)))
	}
	tabs.OnSelected = func(it *container.TabItem) {
		if f := u.tabHooks[it.Text]; f != nil {
			f()
		}
	}
	return tabs
}

func init() {
	registerTab(10, "Play", (*ui).playTab)
	registerTab(20, "Install / Update", (*ui).installTab)
	registerTab(30, "Mods", (*ui).modsTab)
	registerTab(40, "Settings", (*ui).settingsTab)
	registerTab(50, "Tools", (*ui).toolsTab)
}
