package main

import (
	"encoding/json"
	"html/template"
	"strings"
)

type widgetItem struct {
	ID      int
	Subject string
	Meta    string
	Text    string
}

// Состояние виджета: только список выполненных ДЗ. Лимит chat_data — 1024 байта.
type widgetState struct {
	Done []int `json:"d"`
}

func encodeState(done []int) string {
	if len(done) > 120 {
		done = done[len(done)-120:]
	}
	b, err := json.Marshal(widgetState{Done: done})
	if err != nil || len(b) > 1024 {
		return `{"d":[]}`
	}
	return string(b)
}

func renderWidget(items []widgetItem) (string, error) {
	var sb strings.Builder
	err := widgetTpl.Execute(&sb, struct{ Items []widgetItem }{items})
	return sb.String(), err
}

var widgetTpl = template.Must(template.New("hw").Parse(widgetHTML))

// Один самодостаточный документ: без внешних ресурсов, без vh/vmin,
// один тап = один TelegramWidget.request (≤64 байта).
const widgetHTML = `<!doctype html><meta charset="utf-8">
<style>
body{margin:0;padding:2px;font:14px/1.4 -apple-system,system-ui,sans-serif;color:var(--tg-theme-text-color,#111)}
.c{border:1px solid rgba(128,128,128,.35);border-radius:10px;padding:8px;margin-bottom:8px}
.s{font-weight:600}
.m{opacity:.6;font-size:12px;margin:2px 0 4px}
.t{white-space:pre-wrap}
.ok{opacity:.5}
.ok .t{text-decoration:line-through}
button{margin-top:6px;padding:5px 10px;border-radius:8px;border:1px solid rgba(128,128,128,.4);background:transparent;color:inherit;font:inherit}
#e{color:#d33;font-size:12px}
</style>
<div id="l">
{{range .Items}}<div class="c" id="c{{.ID}}"><div class="s">{{.Subject}}</div><div class="m">{{.Meta}}</div><div class="t">{{.Text}}</div><button id="b{{.ID}}" onclick="hit({{.ID}})">✔ Выполнено</button></div>
{{end}}</div>
<div id="e"></div>
<script>
var W=window.TelegramWidget,D={},IDS=[{{range .Items}}{{.ID}},{{end}}0];
function paint(){for(var i=0;i<IDS.length;i++){var id=IDS[i];if(!id)continue;var c=document.getElementById('c'+id),b=document.getElementById('b'+id);if(!c||!b)continue;c.className=D[id]?'c ok':'c';b.textContent=D[id]?'↩ Вернуть':'✔ Выполнено';}}
function apply(s){try{var j=JSON.parse(s||'{}');D={};var a=j.d||[];for(var i=0;i<a.length;i++)D[a[i]]=1;}catch(e){}paint();}
function err(t){document.getElementById('e').textContent=t;}
function hit(id){var b=document.getElementById('b'+id);if(!W||!b)return;b.disabled=true;err('');
W.request('d:'+id).then(function(r){apply(r&&(r.result!==undefined?r.result:r.data));b.disabled=false;},function(e){err('Не удалось: '+e);b.disabled=false;});}
if(W){W.onEvent('chatDataChanged',function(){apply(W.chatData)});apply(W.chatData);W.ready();}
</script>`
