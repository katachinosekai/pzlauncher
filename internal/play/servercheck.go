package play

import (
	"strconv"
	"strings"
)

func (st ServerStatus) keywordVersion() string {
	for _, kw := range strings.Split(st.Keywords, ";") {
		if v, ok := strings.CutPrefix(kw, "VERSION:"); ok {
			return v
		}
	}
	return ""
}

func (st ServerStatus) Version() string {
	if v, ok := st.Rules["version"]; ok {
		return v
	}
	return st.keywordVersion()
}

func (st ServerStatus) hasKeyword(k string) bool {
	for _, kw := range strings.Split(st.Keywords, ";") {
		if kw == k {
			return true
		}
	}
	return false
}

func (st ServerStatus) Open() bool {
	if v, ok := st.Rules["open"]; ok {
		return v != "0"
	}
	return !st.hasKeyword("closed")
}

func (c *Checker) CheckServer(srv Server, acct *Account, st ServerStatus) CheckResult {
	var r CheckResult
	c.checkLauncher(&r)
	if !st.Online {
		r.add(SevError, "Server is offline (no reply on %s).", srv.Addr())
		return r
	}
	if !st.HasPlayers {
		r.add(SevWarning, "Server answers pings but not Steam queries; version, players and password could not be checked.")
	}
	if st.HasPlayers && st.MaxPlayers > 0 && st.Players >= st.MaxPlayers {
		r.add(SevError, "Server is full (%d/%d players).", st.Players, st.MaxPlayers)
	}
	gameOK := c.GameErr == nil
	if !gameOK && st.HasPlayers {
		r.add(SevWarning, "Could not read the installed game version (%v); version check skipped.", c.GameErr)
	}
	if v := st.Version(); v != "" && gameOK && v != c.Game.Version.String() {
		r.add(SevError, "Version mismatch: server runs %s, your game is %s. The server refuses clients with a different version.", v, c.Game.Version)
	}
	if acct == nil {
		r.add(SevWarning, "No saved account for this server. Join it once from the in-game server list with \"save password\" ticked, or auto-join stops at the menu.")
	} else if !acct.PasswordSaved {
		r.add(SevWarning, "The password for %s is not saved. Join once from the in-game server list with \"save password\" ticked, or auto-join stops at the login.", acct.Username)
	}
	if st.Password && !srv.ServerPasswordSaved {
		r.add(SevWarning, "Server needs a server password and none is saved in the server list.")
	}
	if st.HasPlayers && !st.Open() && acct == nil {
		r.add(SevWarning, "Server is closed to new players (whitelist); you need an existing account on it.")
	}
	if n, ok := st.Rules["modCount"]; ok {
		if cnt, err := strconv.Atoi(n); err == nil && cnt > 0 {
			ids, truncated := ServerModsPrefix(st.Rules)
			var missing []string
			if c.Mods != nil {
				for _, id := range ids {
					if c.Mods.Get(id) == nil {
						missing = append(missing, id)
					}
				}
			}
			msg := "Server uses " + n + " mods; the game downloads missing Workshop items itself when joining."
			if truncated || len(ids) < cnt {
				msg += " Steam only shows the first " + strconv.Itoa(len(ids)) + ", so the rest are not checked here."
			}
			r.add(SevInfo, "%s", msg)
			if len(missing) > 0 {
				r.add(SevInfo, "Not installed yet, will be downloaded on join: %s.", strings.Join(missing, ", "))
			}
		}
	}
	if v := st.Version(); v != "" && !r.Blocking() {
		r.add(SevInfo, "Server version %s.", v)
	}
	return r
}

func CheckServer(srv Server, acct *Account, st ServerStatus, e *Env) CheckResult {
	return e.NewChecker().CheckServer(srv, acct, st)
}
