package webview

import (
	"bufio"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"
)

const (
	OpSignIn       = "signin"
	OpCancelSignIn = "cancelsignin"
	OpSignOut      = "signout"
	OpState        = "state"
	OpSubscribe    = "subscribe"
	OpItem         = "item" // an action on one Workshop item's page (see Act*)
	OpQuit         = "quit"
)

const (
	ModeWindow = "window"
	ModeQR     = "qr"
)

const (
	EvReady        = "ready"
	EvUnavailable  = "unavailable"
	EvExternal     = "external"
	EvSession      = "session"
	EvQR           = "qr"
	EvSignInClosed = "signinclosed"
	EvSubResult    = "subresult"
	EvItemResult   = "itemresult"
	EvAwardClosed  = "awardclosed" // the Steam award window was closed
)

// OpItem actions, done on the item's Steam page in the signed-in browser.
const (
	ActRead     = "read" // current vote + favorite
	ActVoteUp   = "voteup"
	ActVoteDown = "votedown"
	ActFav      = "favorite"
	ActUnfav    = "unfavorite"
	ActColls    = "collections"    // list your collections, marking the ones holding the item
	ActSetColls = "setcollections" // add to / remove from collections (Colls)
	ActAward    = "award"          // show Steam's own award window for the item
)

// Item state in EvItemResult. Steam has no "remove vote": clicking the vote
// already given does nothing.
const (
	VoteUp   = "up"
	VoteDown = "down"
	VoteNone = "none"
	FavYes   = "yes"
	FavNo    = "no"
)

// CollChange adds the item to (Add) or removes it from one of your collections.
type CollChange struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Add   bool   `json:"add"`
}

// Coll is one of your collections; In = it already holds the item.
type Coll struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	In    bool   `json:"in"`
}

const maxColls = 500

const (
	ResultDone        = "done"
	ResultAlready     = "already"
	ResultNotSignedIn = "not_signed_in"
	ResultFailed      = "failed"
)

const (
	StatusURL = "https://steamcommunity.com/app/108600/workshop/"
	LoginURL  = "https://steamcommunity.com/login/home/?goto=%2Fapp%2F108600%2Fworkshop%2F"
)

func ItemURL(id string) string {
	return "https://steamcommunity.com/sharedfiles/filedetails/?id=" + url.QueryEscape(id)
}

type Command struct {
	Op          string       `json:"op"`
	Mode        string       `json:"mode,omitempty"`
	ID          string       `json:"id,omitempty"`
	Req         int          `json:"req,omitempty"`
	Unsubscribe bool         `json:"unsubscribe,omitempty"`
	Deps        bool         `json:"deps,omitempty"`
	Action      string       `json:"action,omitempty"` // OpItem
	Colls       []CollChange `json:"colls,omitempty"`  // OpItem ActSetColls
}

type Event struct {
	Event     string `json:"event"`
	URL       string `json:"url,omitempty"`
	ID        string `json:"id,omitempty"`
	Req       int    `json:"req,omitempty"`
	Result    string `json:"result,omitempty"`
	SignedIn  bool   `json:"signed_in,omitempty"`
	Name      string `json:"name,omitempty"`
	Image     string `json:"image,omitempty"`
	Challenge string `json:"challenge,omitempty"`
	Error     string `json:"error,omitempty"`
	Vote      string `json:"vote,omitempty"`  // EvItemResult: up / down / none ("" = page has no vote buttons)
	Fav       string `json:"fav,omitempty"`   // EvItemResult: yes / no
	Colls     []Coll `json:"colls,omitempty"` // EvItemResult for ActColls
}

const maxLine = 1 << 20

var (
	digits      = regexp.MustCompile(`^[0-9]{1,20}$`)
	challengeRe = regexp.MustCompile(`^https://s\.team/q/[0-9]{1,10}/[0-9]{1,30}$`)
)

func (c Command) Valid() bool {
	switch c.Op {
	case OpCancelSignIn, OpSignOut, OpState, OpQuit:
		return true
	case OpSignIn:
		return c.Mode == ModeWindow || c.Mode == ModeQR
	case OpSubscribe:
		return digits.MatchString(c.ID)
	case OpItem:
		if !digits.MatchString(c.ID) {
			return false
		}
		switch c.Action {
		case ActRead, ActVoteUp, ActVoteDown, ActFav, ActUnfav, ActColls, ActAward:
			return len(c.Colls) == 0
		case ActSetColls:
			if len(c.Colls) == 0 || len(c.Colls) > maxColls {
				return false
			}
			for _, x := range c.Colls {
				if !digits.MatchString(x.ID) || len(x.Title) > 512 {
					return false
				}
			}
			return true
		}
	}
	return false
}

func writeLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

func WriteCommand(w io.Writer, c Command) error { return writeLine(w, c) }

func WriteEvent(w io.Writer, e Event) error { return writeLine(w, e) }

func scanLines(r io.Reader, fn func([]byte)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), maxLine)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			fn(sc.Bytes())
		}
	}
	return sc.Err()
}

func ReadCommands(r io.Reader, fn func(Command)) error {
	return scanLines(r, func(b []byte) {
		var c Command
		if json.Unmarshal(b, &c) == nil && c.Valid() {
			fn(c)
		}
	})
}

func ReadEvents(r io.Reader, fn func(Event)) error {
	return scanLines(r, func(b []byte) {
		var e Event
		if json.Unmarshal(b, &e) == nil && e.Event != "" {
			fn(e)
		}
	})
}

const (
	MsgPage   = "page"
	MsgQR     = "qr"
	MsgResult = "result"
)

type ScriptMsg struct {
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	SignedIn  bool   `json:"signedIn"`
	Name      string `json:"name"`
	ItemID    string `json:"item"`
	Image     string `json:"image"`
	Challenge string `json:"challenge"`
	ID        string `json:"id"`
	Result    string `json:"result"`
	Error     string `json:"error"`
	Diag      string `json:"diag"`
	Vote      string `json:"vote"`
	Fav       string `json:"fav"`
	Colls     []Coll `json:"colls"`
}

const maxQRImage = 512 << 10

func ParseScriptMessage(s string) (ScriptMsg, bool) {
	var m ScriptMsg
	if len(s) > maxQRImage+4096 || json.Unmarshal([]byte(s), &m) != nil {
		return ScriptMsg{}, false
	}
	m.Name = cleanText(m.Name, 64)
	m.Error = cleanText(m.Error, 300)
	switch m.Kind {
	case MsgPage:
		return m, m.ItemID == "" || digits.MatchString(m.ItemID)
	case MsgQR:
		if !challengeRe.MatchString(m.Challenge) {
			m.Challenge = ""
		}
		if !strings.HasPrefix(m.Image, "data:image/png;base64,") && !strings.HasPrefix(m.Image, "data:image/gif;base64,") && !strings.HasPrefix(m.Image, "data:image/jpeg;base64,") {
			m.Image = ""
		}
		return m, m.Image != "" || m.Challenge != ""
	case MsgResult:
		switch m.Result {
		case ResultDone, ResultAlready, ResultNotSignedIn, ResultFailed:
			if !digits.MatchString(m.ID) || len(m.Colls) > maxColls {
				return ScriptMsg{}, false
			}
			switch m.Vote {
			case "", VoteUp, VoteDown, VoteNone:
			default:
				return ScriptMsg{}, false
			}
			switch m.Fav {
			case "", FavYes, FavNo:
			default:
				return ScriptMsg{}, false
			}
			var colls []Coll
			for _, c := range m.Colls {
				if digits.MatchString(c.ID) {
					colls = append(colls, Coll{ID: c.ID, Title: cleanText(c.Title, 128), In: c.In})
				}
			}
			m.Colls = colls
			return m, true
		}
	}
	return ScriptMsg{}, false
}

func cleanText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return s
}

func PageScript(post string) string {
	return `(function(){if(window.__pzl)return;window.__pzl=1;` +
		`function post(o){try{` + post + `(JSON.stringify(o));}catch(e){}}` +
		`var login=/^\/login/.test(location.pathname);var chal='';` +
		`function scan(s){var m=/https:\/\/s\.team\/q\/[0-9]+\/[0-9]+/.exec(s);if(m)chal=m[0];}` +
		`function bytes(buf){try{var a=new Uint8Array(buf);var s='';for(var i=0;i<a.length&&i<65536;i++)s+=String.fromCharCode(a[i]);scan(s);}catch(e){}}` +
		`if(login){` +
		`var of=window.fetch;if(of){window.fetch=function(){return of.apply(this,arguments).then(function(r){try{r.clone().arrayBuffer().then(bytes);}catch(e){}return r;});};}` +
		`var oo=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(){this.addEventListener('load',function(){try{if(this.responseType==='arraybuffer')bytes(this.response);else if(this.responseType===''||this.responseType==='text')scan(this.responseText);}catch(e){}});return oo.apply(this,arguments);};` +
		`}` +
		QRFinderJS +
		`function check(){` +
		`var sid=window.g_steamID;var signed=typeof sid==='string'&&sid.length>0;var name='';` +
		`if(!signed){try{var uc=window.UserConfig;if(uc&&typeof uc==='object'){signed=!!(uc.logged_in&&uc.steamid&&uc.steamid!=='0');}}catch(e){}}` +
		`if(!signed){try{var sd=document.getElementById('valve-ssr-data');if(sd){var cfg=JSON.parse(sd.textContent).UserConfig;signed=!!(cfg&&cfg.logged_in&&cfg.steamid&&cfg.steamid!=='0');}}catch(e){}}` +
		`if(!signed){try{var ac=document.getElementById('application_config');var ui=ac&&ac.getAttribute('data-userinfo');if(ui){var u=JSON.parse(ui);signed=!!(u&&u.logged_in&&u.steamid&&u.steamid!=='0');}}catch(e){}}` +
		`var n=document.querySelector('#account_pulldown');if(n)name=n.textContent;` +
		`if(!name){var pn=document.querySelector('[class*="persona_name"],[class*="PersonaName"]');if(pn)name=pn.textContent;}` +
		`var m=/[?&]id=(\d+)/.exec(location.search);` +
		`var item=(m&&/\/(sharedfiles|workshop)\/filedetails/.test(location.pathname))?m[1]:'';` +
		`var dg=[];try{dg.push('g_steamID:'+typeof window.g_steamID+(typeof window.g_steamID==='string'?'('+window.g_steamID.length+')':''));dg.push('ssr:'+!!document.getElementById('valve-ssr-data'));var ac=document.getElementById('application_config');dg.push('appcfg:'+!!ac);if(ac){var at=[];for(var i=0;i<ac.attributes.length;i++)at.push(ac.attributes[i].name);dg.push('appcfg_attrs:'+at.join('|'));}var gk=[];for(var k in window){if(/steam|login|account|user|persona/i.test(k))gk.push(k);}dg.push('globals:'+gk.slice(0,40).join('|'));var ids=[];document.querySelectorAll('[id]').forEach(function(e){if(/account|persona|login|user|avatar/i.test(e.id))ids.push(e.id);});dg.push('ids:'+ids.slice(0,30).join('|'));var cls=[];document.querySelectorAll('[class]').forEach(function(e){var c=String(e.className);if(/persona|account|avatar|login|signin|user_/i.test(c)&&cls.length<20)cls.push(c.slice(0,60));});dg.push('classes:'+cls.join('|'));}catch(e){dg.push('err:'+e);}post({kind:'page',url:location.href,signedIn:signed,name:(name||'').trim(),item:item,diag:dg.join(' ')});` +
		`}function run(){check();` +
		`if(login){var last='';setInterval(function(){var u=findQR();var k=u+'|'+chal;if((u||chal)&&k!==last){last=k;post({kind:'qr',image:u,challenge:chal});}},1000);}` +
		`}` +
		`var ran=0;function again(){if(ran++<3)check();}` +
		`if(document.readyState==='loading'){document.addEventListener('DOMContentLoaded',run);}else{run();}` +
		`window.addEventListener('load',again);setTimeout(again,2500);` +
		`})();`
}

const QRFinderJS = `function fiberChal(el){try{for(var k in el){if(k.indexOf('__reactFiber')===0){var f=el[k];for(var d=0;f&&d<20;d++,f=f.return){var p=f.memoizedProps;if(p){for(var q in p){var v=p[q];if(typeof v==='string'){var m=/https:\/\/s\.team\/q\/[0-9]+\/[0-9]+/.exec(v);if(m)return m[0];}}}}}}}catch(e){}return '';}` +
	`function findQR(){` +
	`var ims=document.querySelectorAll('img');for(var j=0;j<ims.length;j++){var im=ims[j];` +
	`if(!im.complete||im.naturalWidth<21||im.naturalWidth>1024||im.naturalWidth!==im.naturalHeight)continue;` +
	`if(!/^data:image\/(png|gif|jpeg);base64,/.test(im.src))continue;` +
	`var c=fiberChal(im);if(c)chal=c;return im.src;}` +
	`var cs=document.querySelectorAll('canvas');for(var i=0;i<cs.length;i++){var cv=cs[i];` +
	`if(cv.width>=21&&cv.width===cv.height&&cv.getBoundingClientRect().width>0){var c2=fiberChal(cv);if(c2)chal=c2;try{return cv.toDataURL('image/png');}catch(e){return '';}}}` +
	`return '';}`

func SubscribeScript(id string, unsubscribe, deps bool, post string) string {
	b, _ := json.Marshal(struct {
		ID   string `json:"id"`
		Want bool   `json:"want"`
		Deps bool   `json:"deps"`
	}{id, !unsubscribe, deps})
	return `(function(a){` +
		`function post(o){o.kind='result';o.id=a.id;try{` + post + `(JSON.stringify(o));}catch(e){}}` +
		`var sid=window.g_steamID;if(!(typeof sid==='string'&&sid.length>0)){post({result:'not_signed_in'});return;}` +
		`var b=document.getElementById('SubscribeItemBtn');if(!b){post({result:'failed',error:'Steam shows no Subscribe button for this item'});return;}` +
		`function on(){return b.classList.contains('toggled');}` +
		`if(on()===a.want){post({result:'already'});return;}` +
		`b.click();var t=0;var iv=setInterval(function(){t++;` +
		`if(on()===a.want){clearInterval(iv);post({result:'done'});return;}` +
		`var d=document.querySelector('.newmodal');` +
		`if(d&&!d.__pzl){d.__pzl=1;var all=d.querySelector('.btn_blue_steamui');var ok=d.querySelector('.btn_green_steamui');` +
		`if(all&&ok){(a.deps?all:ok).click();}else{var c=d.querySelector('.newmodal_content');clearInterval(iv);` +
		`post({result:'failed',error:c?c.textContent:'Steam showed an error'});if(ok)ok.click();return;}}` +
		`if(t>=50){clearInterval(iv);post({result:'failed',error:'Steam did not confirm the change'});}` +
		`},300);` +
		`})(` + string(b) + `);`
}

// ItemScript runs one OpItem action on the item's Steam page. Votes and the
// favorite click Steam's own buttons (#VoteUpBtn, #VoteDownBtn,
// #FavoriteItemBtn; "toggled" marks the pressed one) and wait for the page to
// show the change. Collections use the page's own calls (ajaxgetmycollections,
// ajaxaddtocollections). Award opens Steam's award window (PublishedFileAward);
// the helper shows the browser window for it. Every answer carries the
// current vote and favorite.
func ItemScript(id, action string, colls []CollChange, post string) string {
	if colls == nil {
		colls = []CollChange{}
	}
	b, _ := json.Marshal(struct {
		ID    string       `json:"id"`
		Act   string       `json:"act"`
		Colls []CollChange `json:"colls"`
	}{id, action, colls})
	return `(function(a){` +
		`function post(o){o.kind='result';o.id=a.id;try{` + post + `(JSON.stringify(o));}catch(e){}}` +
		`var sid=window.g_steamID;if(!(typeof sid==='string'&&sid.length>0)){post({result:'not_signed_in'});return;}` +
		`var up=document.getElementById('VoteUpBtn');var dn=document.getElementById('VoteDownBtn');var fv=document.getElementById('FavoriteItemBtn');` +
		`function vote(){if(!up||!dn)return '';return up.classList.contains('toggled')?'up':(dn.classList.contains('toggled')?'down':'none');}` +
		`function fav(){return fv?(fv.classList.contains('toggled')?'yes':'no'):'';}` +
		`function done(r,x,extra){var o={result:r,vote:vote(),fav:fav()};if(x)o.error=x;if(extra)o.colls=extra;post(o);}` +
		`function fail(x){done('failed',x);}` +
		`function form(o){var p=new URLSearchParams();for(var k in o)p.append(k,o[k]);return p;}` +
		`function call(u,o){return fetch(u,{method:'POST',credentials:'include',body:form(o)});}` +
		`var ses=window.g_sessionID||'';` +
		`if(a.act==='read'){done('done');return;}` +
		`if(a.act==='collections'){call('https://steamcommunity.com/sharedfiles/ajaxgetmycollections',{appid:'108600',publishedfileid:a.id,sessionid:ses})` +
		`.then(function(r){return r.json();}).then(function(j){var inn={};` +
		`(((j||{}).parent_collections||{}).publishedfiledetails||[]).forEach(function(d){inn[d.publishedfileid]=1;});var out=[];` +
		`(((j||{}).all_collections||{}).publishedfiledetails||[]).forEach(function(d){if(d.result!=1||d.publishedfileid==a.id)return;out.push({id:String(d.publishedfileid),title:String(d.title||''),in:!!inn[d.publishedfileid]});});` +
		`done('done','',out);}).catch(function(){fail('Steam did not list your collections');});return;}` +
		`if(a.act==='setcollections'){var p={sessionID:ses,publishedfileid:a.id};a.colls.forEach(function(c){var k='collections['+c.id+']';p[k+(c.add?'[add]':'[remove]')]='true';p[k+'[title]']=c.title;});` +
		`call('https://steamcommunity.com/sharedfiles/ajaxaddtocollections',p).then(function(r){` +
		`if(r.ok){done('done');return;}return r.json().then(function(j){var m=[];(((j||{}).results)||[]).forEach(function(x){if(x&&x.error)m.push(x.error);});fail(m.join('; ')||'Steam did not change the collections');});` +
		`}).catch(function(){fail('Steam did not change the collections');});return;}` +
		`if(a.act==='award'){if(typeof window.PublishedFileAward!=='function'){fail('Steam shows no Award button for this item');return;}` +
		`var ft=0;var els=document.querySelectorAll('[onclick*="PublishedFileAward"]');for(var i=0;i<els.length;i++){var r=/PublishedFileAward\(\s*'?\d+'?\s*,\s*(\d+)/.exec(els[i].getAttribute('onclick')||'');if(r){ft=parseInt(r[1],10);break;}}` +
		`window.PublishedFileAward(a.id,ft);done('done');return;}` +
		`var want,el,get;` +
		`if(a.act==='voteup'||a.act==='votedown'){if(!up||!dn){fail('Steam shows no vote buttons for this item');return;}want=a.act==='voteup'?'up':'down';el=want==='up'?up:dn;get=vote;}` +
		`else if(a.act==='favorite'||a.act==='unfavorite'){if(!fv){fail('Steam shows no Favorite button for this item');return;}want=a.act==='favorite'?'yes':'no';el=fv;get=fav;}` +
		`else{fail('unknown action');return;}` +
		`if(get()===want){done('already');return;}` +
		`el.click();var t=0;var iv=setInterval(function(){t++;` +
		`if(get()===want){clearInterval(iv);done('done');return;}` +
		`var d=document.querySelector('.newmodal');` +
		`if(d&&!d.__pzl){d.__pzl=1;var c=d.querySelector('.newmodal_content');clearInterval(iv);` +
		`fail(c?c.textContent:'Steam showed an error');` +
		`var ok=d.querySelector('.btn_green_steamui,.btn_grey_steamui,.newmodal_close');if(ok)ok.click();return;}` +
		`if(t>=50){clearInterval(iv);fail('Steam did not confirm the change');}` +
		`},300);` +
		`})(` + string(b) + `);`
}
