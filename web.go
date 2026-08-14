package main

import "net/http"

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>fanout</title>
<style>
:root{
  --bg:#12151a; --panel:#181c23; --line:#262c36; --text:#dde3ec;
  --dim:#8b95a5; --accent:#4a9eda; --ok:#3fa66b; --warn:#c9903a; --bad:#c25450;
}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);
  font:13px/1.5 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
header{display:flex;align-items:center;gap:16px;padding:10px 16px;
  border-bottom:1px solid var(--line);background:var(--panel)}
h1{font-size:13px;font-weight:600;margin:0;letter-spacing:0}
.spacer{flex:1}
button{font:inherit;color:var(--text);background:#222833;border:1px solid var(--line);
  border-radius:4px;padding:4px 10px;cursor:pointer;display:inline-flex;
  align-items:center;gap:5px;white-space:nowrap}
button:hover:not(:disabled){border-color:var(--accent)}
button:disabled{opacity:.45;cursor:default}
button.primary{background:var(--accent);border-color:var(--accent);color:#0b0e12;font-weight:600}
button.icon{padding:3px 6px;background:transparent;border-color:transparent;color:var(--dim)}
button.icon:hover:not(:disabled){color:var(--accent);border-color:var(--line)}
button.icon.danger:hover:not(:disabled){color:var(--bad);border-color:rgba(194,84,80,.35)}
svg{width:14px;height:14px;stroke:currentColor;fill:none;stroke-width:1.8;
  stroke-linecap:round;stroke-linejoin:round;flex:none}
main{padding:14px 16px 40px;max-width:1180px;margin:0 auto}
.bar{display:flex;align-items:center;gap:10px;margin-bottom:12px}
.bar h2{font-size:12px;margin:0;font-weight:600;color:var(--dim)}
.exit{border:1px solid var(--line);border-radius:6px;margin-bottom:8px;
  background:var(--panel);overflow:hidden}
.exit>.row{display:grid;gap:6px 12px;align-items:center;padding:9px 12px;
  grid-template-columns:14px minmax(132px,auto) 1fr auto auto auto;
  grid-template-areas:"dot ip meta chips socks acts"}
.exit .dot{grid-area:dot}
.exit .ip{grid-area:ip}
.exit .meta{grid-area:meta}
.exit .chips{grid-area:chips}
.exit .socks{grid-area:socks}
.exit .acts{grid-area:acts}
.dot{width:8px;height:8px;border-radius:50%;background:var(--dim);justify-self:center}
.dot.up{background:var(--ok)}
.dot.starting{background:var(--warn);animation:pulse 1.2s ease-in-out infinite}
.dot.failed{background:var(--bad)}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:.3}}
.ip{font-weight:600;font-variant-numeric:tabular-nums}
.meta{color:var(--dim);font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.chips{display:flex;gap:6px;flex-wrap:wrap}
.chip{border:1px solid var(--line);border-radius:3px;padding:1px 7px;font-size:11px;
  color:var(--dim);cursor:pointer;background:#0e1116}
.chip:hover{border-color:var(--accent);color:var(--text)}
.chip.none{border-style:dashed;cursor:default}
.chip.none:hover{border-color:var(--line);color:var(--dim)}
.orphan{margin-top:18px;border:1px solid var(--line);border-radius:6px;
  background:var(--panel);padding:10px 12px}
.orphan .top{display:flex;align-items:center;gap:10px;margin-bottom:8px}
.orphan .top h3{font-size:12px;margin:0;font-weight:600;color:var(--dim)}
.socks{color:var(--dim);font-size:12px;font-variant-numeric:tabular-nums}
.socks button{background:transparent;border-color:transparent;color:var(--dim);
  font-size:12px;padding:2px 6px;font-variant-numeric:tabular-nums}
.socks button:hover:not(:disabled){color:var(--accent);border-color:var(--line)}
.socks button .lock{width:11px;height:11px;stroke-width:2}
.acts{display:flex;gap:2px;justify-self:end}
.errline{padding:0 12px 9px 38px;color:var(--bad);font-size:11px;
  overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.empty{border:1px dashed var(--line);border-radius:6px;padding:40px 20px;
  text-align:center;color:var(--dim)}
.empty button{margin-top:14px}
.jobs{margin-bottom:12px}
.job{border:1px solid var(--line);border-radius:6px;background:var(--panel);
  padding:10px 12px;margin-bottom:8px}
.job .top{display:flex;align-items:center;gap:10px;margin-bottom:8px}
.job .top strong{font-weight:600;font-size:12px}
.steps{display:flex;flex-wrap:wrap;gap:6px}
.step{display:flex;align-items:center;gap:5px;font-size:11px;color:var(--dim);
  border:1px solid var(--line);border-radius:3px;padding:2px 7px;background:#0e1116}
.step.ok{color:var(--ok);border-color:rgba(63,166,107,.35)}
.step.failed{color:var(--bad);border-color:rgba(194,84,80,.35)}
.step.running{color:var(--warn);border-color:rgba(201,144,58,.35)}
.spin{animation:rot 1s linear infinite;transform-origin:center}
@keyframes rot{to{transform:rotate(360deg)}}
.links{display:flex;gap:14px;margin-right:4px}
.links a{color:var(--dim);text-decoration:none;font-size:12px}
.links a:hover{color:var(--accent)}
@media(max-width:820px){.links{display:none}
  main{padding:12px 12px 40px}
  .exit>.row{grid-template-columns:14px 1fr auto;
    grid-template-areas:"dot ip acts" ". meta meta" ". socks socks" ". chips chips"}
  .exit .chips{margin-top:2px}
  .bar{flex-wrap:wrap}}
.modal{position:fixed;inset:0;background:rgba(8,10,14,.72);display:none;
  align-items:center;justify-content:center;z-index:50;padding:20px}
.modal.open{display:flex}
.sheet{background:var(--bg);border:1px solid var(--line);border-radius:6px;
  width:min(680px,100%);max-height:86vh;display:flex;flex-direction:column}
.sheet .head{display:flex;align-items:center;gap:10px;padding:10px 14px;
  border-bottom:1px solid var(--line);background:var(--panel);border-radius:6px 6px 0 0}
.sheet .head h2{font-size:12px;margin:0;font-weight:600}
.sheet .body{overflow:auto;padding:14px}
.sheet .foot{display:flex;align-items:center;gap:10px;padding:10px 14px;
  border-top:1px solid var(--line);background:var(--panel);border-radius:0 0 6px 6px}
.count{color:var(--dim);font-size:11px}
label.f{display:block;margin-bottom:16px}
label.f[hidden]{display:none}
label.f>span{display:block;color:var(--dim);font-size:11px;margin-bottom:6px}
.regions{display:grid;grid-template-columns:repeat(auto-fill,minmax(148px,1fr));
  gap:6px;max-height:224px;overflow:auto}
.rg{border:1px solid var(--line);background:#0e1116;border-radius:4px;padding:7px 9px;
  cursor:pointer;text-align:left;display:block;width:100%}
.rg:hover{border-color:var(--accent)}
.rg.sel{border-color:var(--accent);background:rgba(74,158,218,.1)}
.rg b{font-weight:600;font-size:12px;display:block;overflow:hidden;
  text-overflow:ellipsis;white-space:nowrap}
.rg em{display:block;font-style:normal;color:var(--dim);font-size:11px;margin-top:2px}
.stepper{display:flex;align-items:center;gap:0;width:fit-content;
  border:1px solid var(--line);border-radius:4px;overflow:hidden;background:#0e1116}
.stepper button{border:0;border-radius:0;background:transparent;padding:5px 11px}
select,input[type=search],input[type=text]{font:inherit;background:#0e1116;
  border:1px solid var(--line);color:var(--text);border-radius:4px;
  padding:5px 8px;width:100%}
select:focus,input[type=search]:focus,input[type=text]:focus{outline:none;border-color:var(--accent)}
.stepper input[type=text]{width:56px;text-align:center;font:inherit;background:transparent;
  border:0;border-left:1px solid var(--line);border-right:1px solid var(--line);
  color:var(--text);padding:5px 0;font-variant-numeric:tabular-nums}
.stepper input:focus{outline:none}
.hint{color:var(--dim);font-size:11px;margin-top:6px}
.setrow{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin-top:16px}
.setrow input,.setrow select{width:100%}
.updsec{margin-top:18px;padding-top:14px;border-top:1px solid var(--line)}
.updrow{display:flex;align-items:center;gap:10px}
.updver{font-size:12px;color:var(--text)}
.updver b{font-weight:600}
.updver span{color:var(--dim);margin-left:8px}
.updnotes{margin-top:10px;padding:10px;background:#0e1116;border:1px solid var(--line);
  border-radius:4px;font-size:12px;line-height:1.6;color:var(--dim);white-space:pre-wrap;
  max-height:180px;overflow:auto}
label.chk{display:flex;align-items:center;gap:7px;color:var(--text);font-size:12px;
  cursor:pointer;margin:0}
label.chk input{margin:0}
.hint.bad{color:var(--bad)}
.kv{display:grid;grid-template-columns:76px 1fr;gap:5px 12px;margin:0 0 14px}
.kv dt{color:var(--dim)}
.kv dd{margin:0;word-break:break-all}
.share{padding:10px;background:#0e1116;border:1px solid var(--line);
  border-radius:4px;word-break:break-all;font-size:12px;line-height:1.7;margin-bottom:8px}
.editbar{display:flex;align-items:flex-end;gap:12px;flex-wrap:wrap;
  padding:12px 0;border-top:1px solid var(--line);margin-top:4px}
.ef{display:block}
.ef>span{display:block;color:var(--dim);font-size:11px;margin-bottom:4px}
.ef input{width:150px}
.credrow{display:flex;align-items:flex-end;gap:12px;flex-wrap:wrap;margin-bottom:10px}
.credrow .ef input{width:190px}
.chead{display:flex;align-items:center;gap:10px;margin:14px 0 8px;
  padding-top:12px;border-top:1px solid var(--line)}
.chead h3{font-size:12px;margin:0;font-weight:600;color:var(--dim)}
.client{border:1px solid var(--line);border-radius:4px;padding:8px 10px;margin-bottom:8px}
.orow{display:flex;align-items:center;gap:10px;padding:6px 0}
.orow select{width:200px}
.crow{display:flex;align-items:center;gap:10px}
.cemail{font-weight:600;font-size:12px}
.cid{color:var(--dim);font-size:11px;overflow:hidden;text-overflow:ellipsis;
  white-space:nowrap;max-width:280px}
.client .share{margin:8px 0 0}
.share button{margin-top:8px}
textarea{width:100%;min-height:300px;background:#0e1116;border:1px solid var(--line);
  color:var(--text);border-radius:4px;
  font:12px/1.8 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
  padding:10px 12px;resize:vertical}
textarea:focus{outline:none;border-color:var(--accent)}
.toast{position:fixed;left:50%;bottom:24px;transform:translateX(-50%);
  background:var(--panel);border:1px solid var(--line);border-radius:4px;
  padding:8px 14px;font-size:12px;z-index:80;opacity:0;pointer-events:none;
  transition:opacity .18s}
.toast.show{opacity:1}
.toast.bad{border-color:rgba(194,84,80,.5);color:var(--bad)}
</style>
</head>
<body>
<header>
  <h1>fanout</h1>
  <span class="count" id="panel"></span>
  <span class="spacer"></span>
  <button class="icon" id="settingsBtn" title="Settings">
    <svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
  </button>
  <nav class="links">
    <a href="https://t.me/+ft-zI76oovgwNmRh" target="_blank" rel="noopener">Telegram</a>
    <a href="https://youtube.com/@joeyblog" target="_blank" rel="noopener">YouTube</a>
    <a href="https://joeyblog.net" target="_blank" rel="noopener">Blog</a>
    <a href="https://github.com/ntun7729/fanout" target="_blank" rel="noopener">GitHub</a>
  </nav>
</header>

<main>
  <div class="jobs" id="jobs"></div>

  <div class="bar">
    <h2>Exits</h2>
    <span class="count" id="ecount"></span>
    <span class="spacer"></span>
    <button id="exportAll" title="Export all node links">
      <svg viewBox="0 0 24 24"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="M7 10l5 5 5-5"/><path d="M12 15V3"/></svg>
      Export Links
    </button>
    <button id="stopall" title="Stop all exits">
      <svg viewBox="0 0 24 24"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>
      Stop All
    </button>
    <button id="newnode" title="Create a node with a protocol and port">
      <svg viewBox="0 0 24 24"><path d="M4 7h16"/><path d="M4 12h16"/><path d="M4 17h10"/></svg>
      New Node
    </button>
    <button class="primary" id="newexit">
      <svg viewBox="0 0 24 24"><path d="M12 5v14"/><path d="M5 12h14"/></svg>
      New Exit
    </button>
  </div>

  <div id="list"></div>

  <div id="orphans"></div>
</main>

<div class="modal" id="wizard">
  <div class="sheet">
    <div class="head">
      <h2>New Exit</h2>
      <span class="spacer"></span>
      <button class="icon" data-close="wizard" title="Close">
        <svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
      </button>
    </div>
    <div class="body">
      <label class="f">
        <span>Region</span>
        <input type="search" id="rgfilter" placeholder="Filter regions">
        <div class="regions" id="regions" style="margin-top:6px"></div>
      </label>
      <label class="f">
        <span>Quantity</span>
        <div class="stepper">
          <button id="minus" title="Decrease">
            <svg viewBox="0 0 24 24"><path d="M5 12h14"/></svg>
          </button>
          <input id="count" type="text" inputmode="numeric" value="3">
          <button id="plus" title="Increase">
            <svg viewBox="0 0 24 24"><path d="M12 5v14"/><path d="M5 12h14"/></svg>
          </button>
        </div>
        <div class="hint" id="availhint"></div>
      </label>
      <label class="f" id="tplwrap">
        <span>Node link</span>
        <select id="tpl"></select>
        <div class="hint" id="tplhint"></div>
      </label>
    </div>
    <div class="foot">
      <span class="count" id="wzhint"></span>
      <span class="spacer"></span>
      <button data-close="wizard">Cancel</button>
      <button class="primary" id="go">Start</button>
    </div>
  </div>
</div>

<div class="modal" id="newnodebox">
  <div class="sheet">
    <div class="head">
      <h2>New Node</h2>
      <span class="spacer"></span>
      <button class="icon" data-close="newnodebox" title="Close">
        <svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
      </button>
    </div>
    <div class="body">
      <label class="f">
        <span>Protocol</span>
        <select id="nproto">
          <option value="vless">VLESS</option>
          <option value="vmess">VMess</option>
          <option value="trojan">Trojan</option>
        </select>
      </label>
      <label class="f">
        <span>Transport</span>
        <select id="nnet">
          <option value="tcp">TCP</option>
          <option value="ws">WebSocket</option>
          <option value="grpc">gRPC</option>
          <option value="httpupgrade">HTTPUpgrade</option>
          <option value="xhttp">XHTTP</option>
        </select>
      </label>
      <label class="f">
        <span>Security</span>
        <select id="nsec">
          <option value="none">None</option>
          <option value="tls">TLS</option>
          <option value="reality">REALITY</option>
        </select>
        <div class="hint" id="nsechint"></div>
      </label>
      <label class="f" id="nvisionwrap" hidden>
        <span>Flow control</span>
        <label class="chk"><input type="checkbox" id="nvision"> xtls-rprx-vision</label>
      </label>
      <label class="f" id="nsniwrap" hidden>
        <span>Domain / SNI</span>
        <input id="nsni" type="text" placeholder="Leave blank for localhost and a self-signed certificate">
      </label>
      <label class="f" id="ncertwrap" hidden>
        <span>Certificate path</span>
        <input id="ncert" type="text" placeholder="Leave blank to generate one, e.g. /etc/ssl/x.crt">
      </label>
      <label class="f" id="nkeywrap" hidden>
        <span>Private key path</span>
        <input id="nkey" type="text" placeholder="Enter with the certificate, e.g. /etc/ssl/x.key">
      </label>
      <label class="f" id="ndestwrap" hidden>
        <span>Destination site</span>
        <input id="ndest" type="text" placeholder="Leave blank for www.tesla.com:443">
      </label>
      <label class="f" id="npathwrap" hidden>
        <span id="npathlabel">Path</span>
        <input id="npath" type="text" placeholder="Leave blank to generate automatically">
      </label>
      <label class="f">
        <span>Port</span>
        <input id="nport" type="text" inputmode="numeric" placeholder="Leave blank for a random port">
      </label>
      <label class="f">
        <span>Remark</span>
        <input id="nremark" type="text" placeholder="Leave blank for an automatic name">
      </label>
    </div>
    <div class="foot">
      <span class="count" id="nnhint"></span>
      <span class="spacer"></span>
      <button data-close="newnodebox">Cancel</button>
      <button class="primary" id="ncreate">Create</button>
    </div>
  </div>
</div>

<div class="modal" id="detail">
  <div class="sheet">
    <div class="head">
      <h2 id="dtitle">Node</h2>
      <span class="spacer"></span>
      <button class="icon danger" id="ddel" title="Delete this inbound">
        <svg viewBox="0 0 24 24"><path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
      </button>
      <button class="icon" data-close="detail" title="Close">
        <svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
      </button>
    </div>
    <div class="body" id="dbody"></div>
  </div>
</div>

<div class="modal" id="credbox">
  <div class="sheet">
    <div class="head">
      <h2>SOCKS5 Credentials</h2>
      <span class="count" id="crtitle"></span>
      <span class="spacer"></span>
      <button class="icon" data-close="credbox" title="Close">
        <svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
      </button>
    </div>
    <div class="body">
      <div class="share" id="crurl"></div>
      <div class="credrow">
        <label class="ef"><span>Username</span>
          <input id="cruser" type="text" spellcheck="false"></label>
        <label class="ef"><span>Password</span>
          <input id="crpass" type="text" spellcheck="false"></label>
        <button id="crrand" title="Generate random credentials">
          <svg viewBox="0 0 24 24"><path d="M21 12a9 9 0 1 1-3-6.7L21 8"/><path d="M21 3v5h-5"/></svg>
          Random
        </button>
      </div>
      <div class="hint">Changes take effect immediately without dropping existing sessions. Clients using old credentials must update their configuration.</div>
    </div>
    <div class="foot">
      <button id="crcopy">
        <svg viewBox="0 0 24 24"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
        Copy Address
      </button>
      <span class="spacer"></span>
      <button data-close="credbox">Cancel</button>
      <button class="primary" id="crsave">Save</button>
    </div>
  </div>
</div>

<div class="modal" id="export">
  <div class="sheet">
    <div class="head">
      <h2>Node Links</h2>
      <span class="count" id="excount"></span>
      <span class="spacer"></span>
      <button id="copyall">
        <svg viewBox="0 0 24 24"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
        Copy All
      </button>
      <button class="icon" data-close="export" title="Close">
        <svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
      </button>
    </div>
    <div class="body"><textarea id="exbox" spellcheck="false" readonly></textarea></div>
  </div>
</div>

<div class="modal" id="settings">
  <div class="sheet">
    <div class="head">
      <h2>Settings</h2>
      <span class="spacer"></span>
      <button class="icon" data-close="settings" title="Close">
        <svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
      </button>
    </div>
    <div class="body">
      <label class="f"><span>Access password</span>
        <input id="setPw" type="password" spellcheck="false" autocomplete="new-password" placeholder="Leave blank to keep unchanged"></label>
      <div class="hint">Changing this only affects new logins. This browser session will remain signed in.</div>

      <label class="f" style="margin-top:16px"><span>Access path</span>
        <input id="setPath" type="text" spellcheck="false" placeholder="Leave blank to remove the path prefix"></label>
      <div class="hint" id="setPathHint">The UI is mounted under this path to hide it from simple port scans. Only letters, numbers, - and _ are allowed.</div>

      <label class="f" style="margin-top:16px"><span>Node backend</span>
        <select id="setBackend"></select></label>
      <div class="hint" id="setBackendHint">Choose where nodes come from. fanout can take over 3x-ui or xray-cf-lite when installed, otherwise it uses its built-in Xray.</div>

      <div class="setrow">
        <label class="f" style="margin:0"><span>Listen port</span>
          <input id="setPort" type="text" inputmode="numeric" spellcheck="false"></label>
        <label class="f" style="margin:0"><span>Local listen address</span>
          <select id="setListen">
            <option value="0.0.0.0">All interfaces (0.0.0.0)</option>
            <option value="127.0.0.1">Localhost only (127.0.0.1)</option>
          </select></label>
      </div>
      <div class="hint bad" id="setPortHint">Changing the port or listen address moves the listener. Reopen the UI using the new address after saving.</div>

      <div class="updsec">
        <div class="updrow">
          <div class="updver">Version <b id="updCur">-</b><span id="updLatest"></span></div>
          <span class="spacer"></span>
          <button id="updCheck">Check for Updates</button>
          <button class="primary" id="updApply" hidden>Update to <span id="updApplyVer"></span></button>
        </div>
        <div class="updnotes" id="updNotes" hidden></div>
      </div>
    </div>
    <div class="foot">
      <span class="spacer"></span>
      <button data-close="settings">Cancel</button>
      <button class="primary" id="setSave">Save</button>
    </div>
  </div>
</div>

<div class="toast" id="toast"></div>

<script>
const $ = s => document.querySelector(s);
const ICON = {
  copy:'<svg viewBox="0 0 24 24"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>',
  stop:'<svg viewBox="0 0 24 24"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>',
  redo:'<svg viewBox="0 0 24 24"><path d="M21 12a9 9 0 1 1-3-6.7L21 8"/><path d="M21 3v5h-5"/></svg>',
  ok:'<svg viewBox="0 0 24 24"><path d="M20 6 9 17l-5-5"/></svg>',
  bad:'<svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>',
  run:'<svg viewBox="0 0 24 24" class="spin"><path d="M21 12a9 9 0 1 1-6.2-8.5"/></svg>',
  wait:'<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/></svg>',
  plus:'<svg viewBox="0 0 24 24"><path d="M12 5v14"/><path d="M5 12h14"/></svg>',
  trash:'<svg viewBox="0 0 24 24"><path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>',
  x:'<svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>',
  lock:'<svg viewBox="0 0 24 24" class="lock"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>'
};

// The UI is mounted under a random prefix, so all requests use relative paths.
async function api(path, opts){
  const r = await fetch(path.replace(/^\//, ''), opts);
  const d = await r.json().catch(()=>({}));
  if(!r.ok) throw new Error(d.error || ('HTTP '+r.status));
  return d;
}
function esc(s){ return String(s == null ? '' : s).replace(/[&<>"']/g, c =>
  ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

let toastTimer;
function toast(msg, bad){
  const el = $('#toast');
  el.textContent = msg;
  el.className = 'toast show' + (bad ? ' bad' : '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.className = 'toast'; }, 2400);
}
async function copy(text){
  // navigator.clipboard only exists under HTTPS/localhost, while the panel is
  // commonly opened as http://IP. Keep execCommand as a fallback.
  if(navigator.clipboard && window.isSecureContext){
    try{ await navigator.clipboard.writeText(text); toast('Copied'); return; }
    catch(e){}
  }
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.setAttribute('readonly', '');
  // Keep it inside the viewport but invisible so iOS does not scroll on focus.
  ta.style.cssText = 'position:fixed;top:0;left:0;width:1px;height:1px;opacity:0;padding:0;border:0';
  document.body.appendChild(ta);
  const prev = document.activeElement;
  ta.focus();
  ta.setSelectionRange(0, ta.value.length);
  let ok = false;
  try{ ok = document.execCommand('copy'); }catch(e){}
  ta.remove();
  if(prev && prev.focus) prev.focus();
  toast(ok ? 'Copied' : 'Copy failed. Please select it manually.', !ok);
}

let view = {exits:[], direct:[], panel:'', backend:'', public_ip:''};
let inbounds = [];

// In native mode fanout owns the inbounds and exposes creation controls.
// With 3x-ui, the panel owns them and this UI is read-only for creation.
function isNative(){ return view.backend === 'native'; }
// In xray-cf-lite mode, xray-cf-lite owns nodes and fanout only changes routing.
function isXCL(){ return view.backend === 'xray-cf-lite'; }
const BACKEND_NAME = {'native':'Built-in Xray', '3x-ui':'3x-ui', 'xray-cf-lite':'xray-cf-lite'};
function backendName(){ return BACKEND_NAME[view.backend] || '3x-ui'; }

const STATUS = {up:'Connected', starting:'Connecting', failed:'Failed', stopped:'Stopped'};

function renderExits(){
  const list = $('#list');
  const n = view.exits.length;
  $('#ecount').textContent = n ? n + (n === 1 ? ' exit' : ' exits') : '';
  $('#exportAll').disabled = !view.exits.some(e => e.inbounds && e.inbounds.length);
  $('#stopall').disabled = !n;

  if(!n){
    list.innerHTML = '<div class="empty">No exits yet'
      + '<div><button class="primary" id="newexit2">'
      + '<svg viewBox="0 0 24 24"><path d="M12 5v14"/><path d="M5 12h14"/></svg>'
      + 'New Exit</button></div></div>';
    return;
  }

  list.innerHTML = view.exits.map(e => {
    const label = e.exit_ip || (e.status === 'starting' ? 'Connecting…' : '—');
    const chips = (e.inbounds || []).length
      ? e.inbounds.map(i => '<button class="chip" data-detail="' + i.id + '" title="'
          + esc((i.remark || i.protocol) + ' · ' + i.protocol + ' :' + i.port) + '">'
          + esc(i.protocol) + ' :' + i.port + '</button>').join('')
      : '<span class="chip none">No nodes</span>';
    const err = e.status === 'failed' && e.err
      ? '<div class="errline" title="' + esc(e.err) + '">' + esc(e.err) + '</div>' : '';
    // Show both country code and full name only when they are actually different.
    const place = e.country && e.country.toUpperCase() !== (e.region || '').toUpperCase()
      ? esc(e.region) + ' ' + esc(e.country) : esc(e.region || '—');
    return '<div class="exit">'
      + '<div class="row">'
      +   '<span class="dot ' + e.status + '" title="' + (STATUS[e.status] || e.status) + '"></span>'
      +   '<span class="ip">' + esc(label) + '</span>'
      +   '<span class="meta">' + place + ' · ' + esc(e.host) + '</span>'
      +   '<span class="chips">' + chips + '</span>'
      +   '<span class="socks"><button data-cred="' + e.slot + '" title="SOCKS5 credentials">'
      +     ICON.lock + ':' + e.port + '</button></span>'
      +   '<span class="acts">'
      +     '<button class="icon" data-swap="' + e.slot + '" title="Switch to another node">' + ICON.redo + '</button>'
      +     '<button class="icon" data-stop="' + e.slot + '" title="Stop this exit">' + ICON.stop + '</button>'
      +   '</span>'
      + '</div>' + err + '</div>';
  }).join('');
}

// Inbounds remain in the panel after an exit is stopped and then route directly.
// List them separately so the user can see and clean them up.
function renderOrphans(){
  const box = $('#orphans');
  const list = view.direct || [];
  if(!list.length){ box.innerHTML = ''; return; }
  const hasUp = view.exits.some(e => e.status === 'up');
  box.innerHTML = '<div class="orphan"><div class="top">'
    + '<h3>Unbound inbounds</h3><span class="count">' + list.length + ' · direct connection</span>'
    + '<span class="spacer"></span>'
    + (isXCL() ? ''
        : '<button data-delorphans="1" title="Delete these inbounds">' + ICON.trash + 'Clean Up</button>')
    + '</div>'
    + list.map(i =>
        '<div class="orow">'
        + '<button class="chip" data-detail="' + i.id + '" title="'
        +   esc((i.remark || i.protocol) + ' · ' + i.protocol + ' :' + i.port) + '">'
        +   esc(i.remark || i.protocol) + ' :' + i.port + '</button>'
        + '<span class="spacer"></span>'
        + (hasUp
            ? '<select class="obind" data-tag="' + esc(i.tag) + '">' + exitOptions('') + '</select>'
            : '<span class="dim">Start an exit first</span>')
        + (isXCL() ? ''
            : '<button class="icon danger" data-delone="' + i.id + '" data-name="'
              + esc((i.remark || i.protocol) + ' :' + i.port) + '" title="Delete this inbound">'
              + ICON.trash + '</button>')
        + '</div>').join('')
    + '</div>';
}

function renderJobs(jobs){
  const box = $('#jobs');
  box.innerHTML = jobs.map(j => {
    const steps = j.steps.map(s => {
      const ic = {ok:ICON.ok, failed:ICON.bad, running:ICON.run}[s.status] || ICON.wait;
      const t = s.detail ? s.label + ' — ' + s.detail : s.label;
      return '<span class="step ' + s.status + '" title="' + esc(t) + '">' + ic
        + esc(s.status === 'ok' && s.detail ? s.detail : s.label) + '</span>';
    }).join('');
    const close = j.status === 'running' ? ''
      : '<button class="icon" data-job="' + esc(j.id) + '" title="Close">' + ICON.x + '</button>';
    return '<div class="job"><div class="top"><strong>' + esc(j.summary) + '</strong>'
      + '<span class="count">' + j.done + '/' + j.total + '</span>'
      + '<span class="spacer"></span>' + close + '</div>'
      + '<div class="steps">' + steps + '</div></div>';
  }).join('');
}

async function poll(){
  try{
    view = await api('/api/exits');
    $('#panel').textContent = view.panel
      ? (backendName() + ': ' + view.panel)
      : (view.panel_info || '');
    // xray-cf-lite owns node creation; fanout only selects the exit route.
    $('#newnode').hidden = isXCL();
    // Links are distributed through xray-cf-lite's subscription system.
    $('#exportAll').hidden = isXCL();
    renderExits();
    renderOrphans();
  }catch(e){}
  try{ renderJobs(await api('/api/jobs') || []); }catch(e){}
}

// ---- New exit wizard ----
let regions = [], region = '', regionsLoaded = false;

function openModal(id){ $('#' + id).classList.add('open'); }
function closeModal(id){ $('#' + id).classList.remove('open'); }

document.addEventListener('click', e => {
  const c = e.target.closest('[data-close]');
  if(c) closeModal(c.dataset.close);
});
document.addEventListener('keydown', e => {
  if(e.key === 'Escape') document.querySelectorAll('.modal.open')
    .forEach(m => m.classList.remove('open'));
});
document.querySelectorAll('.modal').forEach(m => {
  m.onclick = e => { if(e.target === m) m.classList.remove('open'); };
});

function renderRegions(){
  const kw = $('#rgfilter').value.trim().toLowerCase();
  const list = regions.filter(r => !kw
    || r.code.toLowerCase().includes(kw) || r.name.toLowerCase().includes(kw));
  $('#regions').innerHTML = ['<button class="rg' + (region === '' ? ' sel' : '')
      + '" data-rg=""><b>Any region</b><em>Prioritize speed</em></button>']
    .concat(list.map(r => '<button class="rg' + (region === r.code ? ' sel' : '')
      + '" data-rg="' + esc(r.code) + '"><b>' + esc(r.code) + ' ' + esc(r.name) + '</b>'
      + '<em>' + r.available + ' available · ' + r.best_speed_mbps.toFixed(0) + ' Mbps</em></button>'))
    .join('');
  updateAvail();
}

function availOf(code){
  if(code === '') return regions.reduce((a, r) => a + r.available, 0);
  const r = regions.find(x => x.code === code);
  return r ? r.available : 0;
}

function updateAvail(){
  const avail = availOf(region);
  const want = Number($('#count').value) || 0;
  const hint = $('#availhint');
  hint.textContent = avail ? avail + ' nodes available' : 'No available nodes in this region';
  hint.className = 'hint' + (want > avail ? ' bad' : '');
  if(want > avail && avail) hint.textContent = 'Only ' + avail + ' remain; all will be used';
  $('#go').disabled = !avail;
}

async function loadWizard(){
  try{
    regions = await api('/api/regions') || [];
    regionsLoaded = true;
    renderRegions();
  }catch(e){ toast('Failed to load regions: ' + e.message, true); }

  const sel = $('#tpl');
  // xray-cf-lite nodes cannot be cloned. The wizard only starts exits in this mode.
  if(isXCL()){
    $('#tplwrap').hidden = true;
    sel.innerHTML = '<option value="0">Create exit only; do not create a node</option>';
    return;
  }
  $('#tplwrap').hidden = false;
  try{
    // Prefer unbound inbounds as templates to avoid repeatedly cloning prior clones.
    const v = await api('/api/exits');
    const free = v.direct || [];
    const bound = (v.exits || []).flatMap(e => e.inbounds || []);
    inbounds = free.concat(bound);
    if(!inbounds.length){
      sel.innerHTML = '<option value="0">No nodes yet</option>';
      $('#tplhint').textContent = 'Create one with New Node first. It can then be used as a template here.';
      return;
    }
    const opt = i => '<option value="' + i.id + '">'
      + esc(i.remark || ('Port ' + i.port)) + ' · ' + esc(i.protocol)
      + ' :' + i.port + '</option>';
    sel.innerHTML =
      (free.length ? '<optgroup label="Unbound exits">' + free.map(opt).join('') + '</optgroup>' : '')
      + (bound.length ? '<optgroup label="Already bound to exits">' + bound.map(opt).join('') + '</optgroup>' : '')
      + '<option value="0">Create exit only; do not create a node</option>';
    $('#tplhint').textContent = 'One copy is created per exit. Client UUIDs stay the same; only the ports differ.';
  }catch(e){
    sel.innerHTML = '<option value="0">' + backendName() + ' unavailable</option>';
    $('#tplhint').textContent = e.message;
  }
}

document.addEventListener('click', e => {
  if(e.target.closest('#newexit') || e.target.closest('#newexit2')){
    openModal('wizard');
    if(!regionsLoaded) loadWizard(); else { renderRegions(); loadWizard(); }
  }
  const rg = e.target.closest('[data-rg]');
  if(rg){ region = rg.dataset.rg; renderRegions(); }
});

// ---- New node ----
document.addEventListener('click', e => {
  if(e.target.closest('#newnode') || e.target.closest('#newnode2')){
    $('#nnhint').textContent = '';
    syncNodeForm();
    openModal('newnodebox');
  }
});

// Show only fields that apply to the selected protocol/transport/security combination.
function syncNodeForm(){
  const proto = $('#nproto').value;
  const net   = $('#nnet').value;
  const sec   = $('#nsec').value;

  // REALITY imitates a TLS handshake and is meaningful only with suitable transports.
  const realityOK = net === 'tcp' || net === 'xhttp' || net === 'grpc';
  const secSel = $('#nsec');
  for(const o of secSel.options){
    if(o.value === 'reality') o.disabled = !realityOK;
  }
  if(secSel.value === 'reality' && !realityOK) secSel.value = 'none';

  const cur = secSel.value;
  $('#nsniwrap').hidden  = cur !== 'tls';
  $('#ncertwrap').hidden = cur !== 'tls';
  $('#nkeywrap').hidden  = cur !== 'tls';
  $('#ndestwrap').hidden = cur !== 'reality';

  // Vision only applies to VLESS + raw TCP + TLS/REALITY.
  const visionOK = proto === 'vless' && net === 'tcp' && cur !== 'none';
  $('#nvisionwrap').hidden = !visionOK;
  if(!visionOK) $('#nvision').checked = false;

  const needPath = net === 'ws' || net === 'httpupgrade' || net === 'xhttp' || net === 'grpc';
  $('#npathwrap').hidden = !needPath;
  $('#npathlabel').textContent = net === 'grpc' ? 'Service name' : 'Path';

  $('#nsechint').textContent =
    cur === 'reality' ? 'Keys and shortId are generated automatically' :
    cur === 'tls'     ? 'If no certificate is provided, a self-signed one is generated and its fingerprint is included in the link' : '';
}
$('#nproto').onchange = syncNodeForm;
$('#nnet').onchange = syncNodeForm;
$('#nsec').onchange = syncNodeForm;

$('#ncreate').onclick = async e => {
  const q = new URLSearchParams({
    protocol: $('#nproto').value,
    network:  $('#nnet').value,
    security: $('#nsec').value,
    port:     ($('#nport').value || '').trim(),
    remark:   ($('#nremark').value || '').trim(),
    path:     ($('#npath').value || '').trim(),
    sni:      ($('#nsni').value || '').trim(),
    cert:     ($('#ncert').value || '').trim(),
    key:      ($('#nkey').value || '').trim(),
    dest:     ($('#ndest').value || '').trim(),
  });
  if($('#nvision').checked) q.set('vision', '1');
  e.target.disabled = true;
  try{
    const r = await api('/api/panel/inbound/new?' + q.toString(), {method:'POST'});
    toast('Created ' + r.protocol + ' node on port ' + r.port);
    closeModal('newnodebox');
    $('#nport').value = '';
    $('#nremark').value = '';
    poll();
  }catch(err){ toast(err.message, true); }
  e.target.disabled = false;
};

$('#rgfilter').oninput = renderRegions;
$('#minus').onclick = () => { step(-1); };
$('#plus').onclick = () => { step(1); };
function step(d){
  const el = $('#count');
  el.value = Math.min(20, Math.max(1, (Number(el.value) || 1) + d));
  updateAvail();
}
$('#count').oninput = updateAvail;

$('#go').onclick = async e => {
  const want = Math.min(Number($('#count').value) || 1, availOf(region) || 1);
  const tpl = $('#tpl').value || '0';
  e.target.disabled = true;
  try{
    await api('/api/provision?count=' + want + '&region=' + encodeURIComponent(region)
      + '&template=' + tpl, {method:'POST'});
    closeModal('wizard');
    poll();
  }catch(err){ toast(err.message, true); }
  e.target.disabled = false;
};

// ---- Exit operations ----
document.addEventListener('click', async e => {
  const stop = e.target.closest('[data-stop]');
  if(stop){
    stop.disabled = true;
    try{ await api('/api/stop?slot=' + stop.dataset.stop, {method:'POST'}); }
    catch(err){ toast(err.message, true); }
    poll();
    return;
  }
  const swap = e.target.closest('[data-swap]');
  if(swap){
    swap.disabled = true;
    try{
      await api('/api/swap?slot=' + swap.dataset.swap, {method:'POST'});
      toast('Switching node');
    }catch(err){ toast(err.message, true); }
    poll();
    return;
  }
  const cred = e.target.closest('[data-cred]');
  if(cred){ openCred(Number(cred.dataset.cred)); return; }
  const job = e.target.closest('[data-job]');
  if(job){
    try{ await api('/api/jobs/dismiss?id=' + job.dataset.job, {method:'POST'}); }catch(err){}
    poll();
    return;
  }
  const del = e.target.closest('[data-delorphans]');
  if(del){
    const list = view.direct || [];
    if(!confirm('Delete these ' + list.length + ' unbound nodes? This cannot be undone.')) return;
    del.disabled = true;
    try{
      await api('/api/xui/delete?ids=' + list.map(i => i.id).join(','), {method:'POST'});
      toast('Cleaned up ' + list.length + ' inbounds');
    }catch(err){ toast(err.message, true); }
    poll();
    return;
  }

  const one = e.target.closest('[data-delone]');
  if(one){
    if(!confirm('Delete inbound ' + one.dataset.name + '? This cannot be undone.')) return;
    one.disabled = true;
    try{
      await api('/api/xui/delete?ids=' + one.dataset.delone, {method:'POST'});
      toast('Deleted ' + one.dataset.name);
    }catch(err){ toast(err.message, true); }
    poll();
  }
});

$('#stopall').onclick = async e => {
  if(!confirm('Stop all ' + view.exits.length + ' exits?')) return;
  e.target.disabled = true;
  for(const x of view.exits){
    try{ await api('/api/stop?slot=' + x.slot, {method:'POST'}); }catch(err){}
  }
  poll();
};

// ---- Node details ----
let curDetail = null;

// Keep detail redraws independent from polling so active edits are not overwritten.
async function openDetail(id){
  $('#dbody').innerHTML = '<div class="empty">Loading…</div>';
  curDetail = null;
  $('#ddel').disabled = true;
  openModal('detail');
  try{
    const d = await api('/api/xui/detail?id=' + id);
    curDetail = d;
    renderDetail(d);
    $('#ddel').hidden = isXCL();
    $('#ddel').disabled = isXCL();
  }catch(err){
    $('#dbody').innerHTML = '<div class="empty">Failed to load: ' + esc(err.message) + '</div>';
  }
}

// Exit selector: connected tunnels plus Direct. Binding uses Xray inboundTag.
function exitOptions(currentHost){
  const up = view.exits.filter(e => e.status === 'up');
  return '<option value=""' + (currentHost ? '' : ' selected') + '>Direct (no tunnel)</option>'
    + up.map(e => '<option value="' + esc(e.host) + '"'
        + (e.host === currentHost ? ' selected' : '') + '>'
        + esc((e.exit_ip || e.host) + ' · ' + e.region) + '</option>').join('');
}

function renderDetail(d){
  const owner = view.exits.find(x => (x.inbounds || []).some(i => i.id === d.id));

  const clients = (d.clients || []).map((c, i) => {
    const link = (d.links || [])[i] || '';
    return '<div class="client">'
      + '<div class="crow">'
      +   '<span class="cemail">' + esc(c.email) + '</span>'
      +   '<span class="cid">' + esc(c.id) + '</span>'
      +   '<span class="spacer"></span>'
      +   (link ? '<button class="icon" data-copy="' + esc(link) + '" title="Copy link">' + ICON.copy + '</button>' : '')
      +   '<button class="icon" data-creset="' + esc(c.email) + '" title="Generate new credentials; the old link stops working immediately">' + ICON.redo + '</button>'
      +   '<button class="icon" data-cdel="' + esc(c.email) + '" title="Delete this client">' + ICON.trash + '</button>'
      + '</div>'
      + (link ? '<div class="share">' + esc(link) + '</div>' : '')
      + '</div>';
  }).join('');

  $('#dtitle').textContent = (d.remark || 'Node') + '  :' + d.port;
  // xray-cf-lite owns its nodes. fanout only exposes exit routing here.
  const editable = !isXCL();
  $('#dbody').innerHTML = '<dl class="kv">'
    + '<dt>Exit</dt><dd><select id="dbind" data-tag="' + esc(d.tag) + '">'
    +   exitOptions(owner ? owner.host : '') + '</select></dd>'
    + '<dt>Protocol</dt><dd>' + esc(d.protocol) + '  ' + esc(d.network || '')
    +   (d.tls && d.tls !== 'none' ? '  ' + esc(d.tls) : '') + '</dd>'
    + '<dt>Listen</dt><dd>' + esc(d.listen || '0.0.0.0') + '</dd>'
    + '</dl>'
    + (editable ? ('<div class="editbar">'
    +   '<label class="ef"><span>Remark</span>'
    +     '<input id="dremark" type="text" value="' + esc(d.remark || '') + '"></label>'
    +   '<label class="ef"><span>Port</span>'
    +     '<input id="dport" type="text" inputmode="numeric" value="' + d.port + '"></label>'
    +   '<label class="chk"><input type="checkbox" id="denable"'
    +     (d.enable === false ? '' : ' checked') + '> Enabled</label>'
    +   '<span class="spacer"></span>'
    +   '<button class="primary" id="dsave">Save</button>'
    + '</div>'
    + '<div class="chead"><h3>Clients</h3><span class="count">'
    +   (d.clients || []).length + '</span><span class="spacer"></span>'
    +   '<button id="dcadd">' + ICON.plus + 'Add</button></div>'
    + (clients || '<div class="empty">No clients</div>'))
    : '<div class="hint">This node is managed by xray-cf-lite. Change its port, UUID, and shared links there. fanout only selects which exit it uses.</div>');
}

// Bind an unbound inbound as soon as an exit is selected.
document.addEventListener('change', async e => {
  const sel = e.target.closest('.obind');
  if(!sel || !sel.value) return;
  sel.disabled = true;
  try{
    await api('/api/xui/bind?tag=' + encodeURIComponent(sel.dataset.tag)
      + '&host=' + encodeURIComponent(sel.value), {method:'POST'});
    toast('Bound');
    poll();
  }catch(err){ toast(err.message, true); sel.disabled = false; }
});

// Detail exit changes take effect immediately. Empty host means direct routing.
document.addEventListener('change', async e => {
  const sel = e.target.closest('#dbind');
  if(!sel) return;
  sel.disabled = true;
  try{
    await api('/api/xui/bind?tag=' + encodeURIComponent(sel.dataset.tag)
      + '&host=' + encodeURIComponent(sel.value), {method:'POST'});
    toast(sel.value ? 'Bound' : 'Unbound');
    poll();
  }catch(err){ toast(err.message, true); }
  sel.disabled = false;
});

document.addEventListener('click', async e => {
  const link = e.target.closest('[data-detail]');
  if(link) return openDetail(link.dataset.detail);

  if(e.target.closest('#dsave')){
    const btn = e.target.closest('#dsave');
    btn.disabled = true;
    const q = new URLSearchParams({
      id: curDetail.id,
      port: ($('#dport').value || '').trim(),
      remark: ($('#dremark').value || '').trim(),
      enable: $('#denable').checked ? '1' : '0',
    });
    try{
      await api('/api/panel/inbound/update?' + q, {method:'POST'});
      toast('Saved');
      await openDetail(curDetail.id);
      poll();
    }catch(err){ toast(err.message, true); btn.disabled = false; }
    return;
  }

  const add = e.target.closest('#dcadd');
  if(add){
    add.disabled = true;
    try{
      await api('/api/panel/client/add?id=' + curDetail.id, {method:'POST'});
      toast('Client added');
      await openDetail(curDetail.id);
    }catch(err){ toast(err.message, true); add.disabled = false; }
    return;
  }

  const del = e.target.closest('[data-cdel]');
  if(del){
    if(!confirm('Delete client ' + del.dataset.cdel + '? Its link will stop working immediately.')) return;
    del.disabled = true;
    try{
      await api('/api/panel/client/del?id=' + curDetail.id
        + '&email=' + encodeURIComponent(del.dataset.cdel), {method:'POST'});
      toast('Deleted');
      await openDetail(curDetail.id);
    }catch(err){ toast(err.message, true); del.disabled = false; }
    return;
  }

  const reset = e.target.closest('[data-creset]');
  if(reset){
    if(!confirm('Reset credentials for ' + reset.dataset.creset + '? Previously distributed links will stop working immediately.')) return;
    reset.disabled = true;
    try{
      await api('/api/panel/client/reset?id=' + curDetail.id
        + '&email=' + encodeURIComponent(reset.dataset.creset), {method:'POST'});
      toast('Reset');
      await openDetail(curDetail.id);
    }catch(err){ toast(err.message, true); reset.disabled = false; }
    return;
  }

  // Delete the current inbound from the detail dialog.
  const dd = e.target.closest('#ddel');
  if(dd && curDetail){
    const name = (curDetail.remark || curDetail.protocol || 'Node') + ' :' + curDetail.port;
    if(!confirm('Delete inbound ' + name + '? All client links will stop working and this cannot be undone.')) return;
    dd.disabled = true;
    try{
      await api('/api/xui/delete?ids=' + curDetail.id, {method:'POST'});
      toast('Deleted ' + name);
      curDetail = null;
      closeModal('detail');
      poll();
    }catch(err){ toast(err.message, true); dd.disabled = false; }
  }
});

document.addEventListener('click', e => {
  const c = e.target.closest('[data-copy]');
  if(c) copy(c.dataset.copy);
});

// ---- SOCKS5 credentials ----
let curCred = null;

function socksURL(host, port, user, pass){
  if(!user) return 'socks5://' + host + ':' + port;
  return 'socks5://' + user + ':' + pass + '@' + host + ':' + port;
}

// SOCKS5 listens on the host running fanout. Clients connect to that host's
// public IPv4, while traffic exits through the tunnel's exit IP.
function credHost(e){
  return view.public_ip || location.hostname || e.host;
}

function openCred(slot){
  const e = view.exits.find(x => x.slot === slot);
  if(!e){ toast('This exit no longer exists', true); return; }
  curCred = {slot: slot, port: e.port, host: credHost(e)};
  $('#crtitle').textContent = e.region + ' · :' + e.port;
  $('#cruser').value = e.socks_user || '';
  $('#crpass').value = e.socks_pass || '';
  refreshCredURL();
  openModal('credbox');
}

function refreshCredURL(){
  if(!curCred) return;
  $('#crurl').textContent = socksURL(curCred.host, curCred.port,
    $('#cruser').value.trim(), $('#crpass').value.trim());
}
$('#cruser').oninput = refreshCredURL;
$('#crpass').oninput = refreshCredURL;

$('#crrand').onclick = () => {
  // Use unambiguous characters that require no escaping on either side.
  const abc = 'abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789';
  const gen = n => Array.from(crypto.getRandomValues(new Uint8Array(n)))
    .map(v => abc[v % abc.length]).join('');
  $('#cruser').value = 'fo' + gen(6);
  $('#crpass').value = gen(14);
  refreshCredURL();
};

$('#crcopy').onclick = () => { copy($('#crurl').textContent); };

$('#crsave').onclick = async e => {
  if(!curCred) return;
  const btn = e.target; btn.disabled = true;
  const q = new URLSearchParams({
    slot: curCred.slot,
    user: $('#cruser').value.trim(),
    pass: $('#crpass').value.trim(),
  });
  try{
    const r = await api('/api/cred?' + q, {method:'POST'});
    $('#cruser').value = r.user;
    $('#crpass').value = r.pass;
    refreshCredURL();
    toast('Saved and active immediately');
    poll();
  }catch(err){ toast(err.message, true); }
  btn.disabled = false;
};

// ---- Export ----
$('#exportAll').onclick = async () => {
  const ids = view.exits.flatMap(x => (x.inbounds || []).map(i => i.id));
  if(!ids.length){ toast('There are no nodes to export yet', true); return; }
  $('#exbox').value = 'Loading…';
  $('#excount').textContent = '';
  openModal('export');
  try{
    const d = await api('/api/xui/links?ids=' + ids.join(','));
    $('#exbox').value = (d.links || []).join('\n');
    $('#excount').textContent = (d.links || []).length + ' links';
  }catch(err){ $('#exbox').value = 'Export failed: ' + err.message; }
};
$('#copyall').onclick = () => { const v = $('#exbox').value; if(v) copy(v); };

// ---- Settings: password / path / port / local listen address ----
let curSettings = null;
let curBackend = null;

// Backend switching: installed modes are selectable; unavailable ones are disabled.
async function loadBackendModes(){
  const sel = $('#setBackend');
  const hint = $('#setBackendHint');
  try{
    const m = await api('/api/panel/mode');
    curBackend = m.mode || '';
    sel.innerHTML = '<option value="">Automatic (based on what is installed)</option>'
      + (m.modes || []).map(x =>
          '<option value="' + esc(x.mode) + '"' + (x.available ? '' : ' disabled')
          + '>' + esc(x.label) + (x.available ? '' : ' (not installed)') + '</option>').join('');
    sel.value = curBackend;
    const bad = (m.modes || []).filter(x => !x.available);
    hint.textContent = m.describe
      ? ('Current: ' + m.describe + (bad.length ? '. Disabled options are not installed on this machine.' : ''))
      : 'Choose where nodes come from. fanout can take over 3x-ui or xray-cf-lite when installed, otherwise it uses its built-in Xray.';
  }catch(err){
    sel.innerHTML = '<option value="">Failed to load</option>';
    hint.textContent = err.message;
  }
}

$('#settingsBtn').onclick = async () => {
  $('#setPw').value = '';
  $('#setPath').value = '';
  $('#setPathHint').textContent = 'Loading…';
  openModal('settings');
  loadBackendModes();
  try{
    const s = await api('/api/settings');
    curSettings = s;
    $('#setPath').value = (s.base_path || '').replace(/^\//, '');
    $('#setPort').value = s.port || '';
    $('#setListen').value = s.listen_addr || '0.0.0.0';
    $('#setPathHint').textContent = 'The UI is mounted under this path to hide it from simple port scans. Only letters, numbers, - and _ are allowed.';
    $('#updCur').textContent = s.version || '-';
    $('#updLatest').textContent = '';
    $('#updNotes').hidden = true;
    $('#updApply').hidden = true;
    $('#updCheck').disabled = false;
    $('#updCheck').textContent = 'Check for Updates';
  }catch(err){ $('#setPathHint').textContent = 'Failed to load: ' + err.message; }
};

// Ask the backend for the latest GitHub release and show update details when available.
$('#updCheck').onclick = async e => {
  e.target.disabled = true;
  e.target.textContent = 'Checking…';
  try{
    const u = await api('/api/update/check');
    $('#updCur').textContent = u.current || '-';
    if(u.has_update){
      $('#updLatest').textContent = 'New version ' + u.latest;
      $('#updApplyVer').textContent = u.latest;
      $('#updApply').hidden = false;
      $('#updNotes').textContent = u.notes || '(No release notes were provided for this version.)';
      $('#updNotes').hidden = false;
    } else {
      $('#updLatest').textContent = 'Up to date';
      $('#updApply').hidden = true;
      $('#updNotes').hidden = true;
    }
  }catch(err){ toast(err.message, true); }
  e.target.disabled = false;
  e.target.textContent = 'Check for Updates';
};

// One-click update downloads the new binary and restarts the service.
$('#updApply').onclick = async e => {
  if(!confirm('Update to ' + $('#updApplyVer').textContent + '? The service will restart and the UI will disconnect briefly.')) return;
  e.target.disabled = true;
  e.target.textContent = 'Updating…';
  try{
    const r = await api('/api/update/apply', {method:'POST'});
    if(r.restarting){
      $('#updNotes').textContent = 'The new version has been downloaded and the service is restarting. Refresh the page in a few seconds.';
      $('#updNotes').hidden = false;
      toast('Updating. Refresh after the service restarts.');
      setTimeout(() => location.reload(), 6000);
    } else {
      toast(r.message || 'Already up to date');
      e.target.disabled = false;
      e.target.textContent = 'Update to ' + $('#updApplyVer').textContent;
    }
  }catch(err){
    toast(err.message, true);
    e.target.disabled = false;
    e.target.textContent = 'Update to ' + $('#updApplyVer').textContent;
  }
};

// If the port/listen address changes, show the new URL. Password/path can apply in place.
function nextURL(port, listen, path){
  const host = (listen && listen !== '0.0.0.0') ? listen : location.hostname;
  return location.protocol + '//' + host + ':' + port + (path ? '/' + path : '') + '/';
}

$('#setSave').onclick = async e => {
  e.target.disabled = true;
  const body = {};
  const pw = $('#setPw').value.trim();
  if(pw) body.password = pw;
  body.base_path = $('#setPath').value.trim();
  const port = parseInt($('#setPort').value.trim(), 10);
  if(port) body.port = port;
  body.listen_addr = $('#setListen').value;

  const portChanged = curSettings && (port !== curSettings.port
    || body.listen_addr !== (curSettings.listen_addr || '0.0.0.0'));

  try{
    // Backend and general settings use separate APIs. Switch backend first so
    // a backend failure does not leave the user thinking everything succeeded.
    const backend = $('#setBackend').value;
    if(curBackend !== null && backend !== curBackend){
      const r = await api('/api/panel/mode', {
        method:'POST',
        headers:{'Content-Type':'application/json'},
        body: JSON.stringify({mode: backend}),
      });
      curBackend = r.mode || '';
      $('#setBackendHint').textContent = 'Current: ' + (r.describe || r.kind || 'switched');
    }
    await api('/api/settings', {
      method:'POST',
      headers:{'Content-Type':'application/json'},
      body: JSON.stringify(body),
    });
    if(portChanged){
      const url = nextURL(port, body.listen_addr, body.base_path);
      $('#setPortHint').innerHTML = 'Listener changed. Open the new address: <a href="' + esc(url) + '">' + esc(url) + '</a>';
      toast('Listener changed. Reopen using the new address.');
    } else {
      toast('Saved');
      // The path may have changed, so reload under the new prefix.
      const np = body.base_path;
      const cur = (curSettings && curSettings.base_path || '').replace(/^\//, '');
      if(np !== cur){
        location.href = location.protocol + '//' + location.host
          + (np ? '/' + np : '') + '/';
        return;
      }
      closeModal('settings');
      poll();
    }
  }catch(err){ toast(err.message, true); }
  e.target.disabled = false;
};

poll();
setInterval(poll, 3000);
</script>
</body>
</html>`