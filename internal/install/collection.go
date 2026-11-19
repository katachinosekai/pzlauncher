package install

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const DefaultCollectionAPI = "https://api.steampowered.com/ISteamRemoteStorage/GetCollectionDetails/v1/"

type collectionChild struct {
	ID       string
	FileType int
}

func fetchCollectionChildren(ctx context.Context, client *http.Client, api, cid string) ([]collectionChild, error) {
	form := url.Values{"collectioncount": {"1"}, "publishedfileids[0]": {cid}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("collection API: %s", resp.Status)
	}
	var body struct {
		Response struct {
			CollectionDetails []struct {
				Children []struct {
					PublishedFileID string `json:"publishedfileid"`
					FileType        int    `json:"filetype"`
				} `json:"children"`
			} `json:"collectiondetails"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if len(body.Response.CollectionDetails) == 0 {
		return nil, fmt.Errorf("no collectiondetails for %s", cid)
	}
	var out []collectionChild
	for _, c := range body.Response.CollectionDetails[0].Children {
		out = append(out, collectionChild{ID: c.PublishedFileID, FileType: c.FileType})
	}
	return out, nil
}

func CollectionItems(ctx context.Context, client *http.Client, api, root string) ([]string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if api == "" {
		api = DefaultCollectionAPI
	}
	seen := map[string]bool{}
	queue := []string{root}
	set := map[string]bool{}
	for len(queue) > 0 {
		cid := queue[0]
		queue = queue[1:]
		if seen[cid] {
			continue
		}
		seen[cid] = true
		children, err := fetchCollectionChildren(ctx, client, api, cid)
		if err != nil {
			return nil, err
		}
		for _, c := range children {
			if c.FileType == 2 {
				if !seen[c.ID] {
					queue = append(queue, c.ID)
				}
			} else {
				set[c.ID] = true
			}
		}
	}
	items := make([]string, 0, len(set))
	for id := range set {
		items = append(items, id)
	}
	sort.Strings(items)
	return items, nil
}

func MissingItems(workshop string, ids []string) []string {
	var out []string
	for _, id := range ids {
		ents, err := os.ReadDir(filepath.Join(workshop, id))
		if err != nil || len(ents) == 0 {
			out = append(out, id)
		}
	}
	return out
}
