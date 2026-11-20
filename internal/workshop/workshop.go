package workshop

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/katachinosekai/pzlauncher/internal/install"
	"github.com/katachinosekai/pzlauncher/internal/paths"
)

const (
	DefaultCollectionID = "3825994973"
	DefaultDetailsAPI   = "https://api.steampowered.com/ISteamRemoteStorage/GetPublishedFileDetails/v1/"
)

type State int

const (
	Installed State = iota
	Downloading
	NotSubscribed
	Unknown
)

func (s State) String() string {
	switch s {
	case Installed:
		return "installed"
	case Downloading:
		return "subscribed, not downloaded yet"
	case NotSubscribed:
		return "not subscribed"
	}
	return "not downloaded"
}

type Item struct {
	ID    string
	Title string
	State State
}

type Options struct {
	CollectionID  string
	CollectionAPI string
	DetailsAPI    string
	Client        *http.Client
	ContentDir    string
	ACFPath       string
}

type Result struct {
	CollectionID string
	ContentDir   string
	ACFFound     bool
	IDs          []string
	Total        int
	Installed    int
	Missing      []Item
	TitleErr     error
}

func Locate() (contentDir, acf string, err error) {
	root, err := paths.SteamRoot()
	if err != nil {
		return "", "", err
	}
	dir, err := paths.WorkshopDir(root)
	if err != nil {
		return "", "", err
	}
	return dir, ACFPathFor(dir), nil
}

func ACFPathFor(contentDir string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(contentDir)), "appworkshop_"+paths.AppID+".acf")
}

func PageURL(id string) string { return "steam://url/CommunityFilePage/" + id }

func Check(ctx context.Context, o Options) (*Result, error) {
	if o.CollectionID == "" {
		o.CollectionID = DefaultCollectionID
	}
	if o.ContentDir == "" {
		dir, acf, err := Locate()
		if err != nil {
			return nil, err
		}
		o.ContentDir = dir
		if o.ACFPath == "" {
			o.ACFPath = acf
		}
	}
	if o.ACFPath == "" {
		o.ACFPath = ACFPathFor(o.ContentDir)
	}
	ids, err := install.CollectionItems(ctx, o.Client, o.CollectionAPI, o.CollectionID)
	if err != nil {
		return nil, err
	}
	r := &Result{CollectionID: o.CollectionID, ContentDir: o.ContentDir, IDs: ids, Total: len(ids)}
	missing := install.MissingItems(o.ContentDir, ids)
	r.Installed = len(ids) - len(missing)
	var acf *ACF
	if b, err := os.ReadFile(o.ACFPath); err == nil {
		if a, err := ParseACF(string(b)); err == nil {
			acf = a
			r.ACFFound = true
		}
	}
	for _, id := range missing {
		it := Item{ID: id, State: Unknown}
		if acf != nil {
			if acf.Subscribed[id] || acf.Installed[id] {
				it.State = Downloading
			} else {
				it.State = NotSubscribed
			}
		}
		r.Missing = append(r.Missing, it)
	}
	if len(missing) > 0 {
		titles, err := FetchTitles(ctx, o.Client, o.DetailsAPI, missing)
		r.TitleErr = err
		for i := range r.Missing {
			r.Missing[i].Title = titles[r.Missing[i].ID]
		}
	}
	sort.SliceStable(r.Missing, func(i, j int) bool {
		if r.Missing[i].State != r.Missing[j].State {
			return r.Missing[i].State > r.Missing[j].State
		}
		return strings.ToLower(r.Missing[i].Title) < strings.ToLower(r.Missing[j].Title)
	})
	return r, nil
}

func FetchTitles(ctx context.Context, client *http.Client, api string, ids []string) (map[string]string, error) {
	det, err := FetchDetails(ctx, client, api, ids)
	out := map[string]string{}
	for id, d := range det {
		if d.Title != "" {
			out[id] = d.Title
		}
	}
	return out, err
}
