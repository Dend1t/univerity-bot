package main

import (
	"encoding/json"
	"html/template"
	"strings"
)

// ---------------------------------------------------------------- состояние (chat_data ≤ 1024 байт)

type wState struct {
	Done  []int          `json:"d,omitempty"`
	Stats map[int][2]int `json:"s,omitempty"`
	Note  string         `json:"n,omitempty"`
}

func encodeState(st wState) string {
	b, err := json.Marshal(st)
	if err == nil && len(b) <= 1024 {
		return string(b)
	}
	b, _ = json.Marshal(wState{Note: st.Note})
	if len(b) > 1024 {
		return "{}"
	}
	return string(b)
}

func pct(done, total int) int {
	if total <= 0 {
		return 0
	}
	return done * 100 / total
}

// ---------------------------------------------------------------- данные шаблонов

type boardItem struct {
	ID            int
	Subject, Meta string
	Text          string
	Mine          bool
}

type dashItem struct {
	ID            int
	Subject, Meta string
	Text          string
	Done, Total   int
	Pct           int
}

type statRow struct {
	Name        string
	Done, Total int
	Pct         int
}

type profileView struct{ Title, Name, Line1, Line2 string }

// ---------------------------------------------------------------- общий каркас

const widgetCSS = `body{margin:0;padding:2px;font:14px/1.4 -apple-system,system-ui,sans-serif;color:var(--tg-theme-text-color,#111)}
.h{font-weight:600;margin-bottom:6px}
.c{border:1px solid rgba(128,128,128,.35);border-radius:10px;padding:8px;margin-bottom:8px}
.s{font-weight:600}
.m{opacity:.6;font-size:12px;margin:2px 0 4px}
.t{white-space:pre-wrap}
.ok{opacity:.5}
.ok .t{text-decoration:line-through}
.gone{display:none}
.bar{height:6px;border-radius:3px;background:rgba(128,128,128,.25);overflow:hidden;margin:4px 0}
.fill{height:6px;background:#3a86ff;width:0}
button{margin:6px 6px 0 0;padding:5px 10px;border-radius:8px;border:1px solid rgba(128,128,128,.4);background:transparent;color:inherit;font:inherit}
#e{color:#d33;font-size:12px}`

const widgetJS = `var W=window.TelegramWidget;
function el(i){return document.getElementById(i)}
function err(t){var e=el('e');if(e)e.textContent=t||''}
function jget(s){try{return JSON.parse(s||'{}')}catch(e){return {}}}
function req(d){return W.request(d).then(function(r){return (r&&(r.result!==undefined?r.result:r.data))||''})}
function boot(f){if(W){W.onEvent('chatDataChanged',function(){f(W.chatData)});f(W.chatData);W.ready()}}`

func newTpl(body string) *template.Template {
	return template.Must(template.New("w").Parse(
		`<!doctype html><meta charset="utf-8"><style>` + widgetCSS + `</style>` + body))
}

func render(t *template.Template, data any) (string, error) {
	var sb strings.Builder
	err := t.Execute(&sb, data)
	return sb.String(), err
}

// ---------------------------------------------------------------- 1. борд ДЗ

var tplBoard = newTpl(`<div class="h" id="hd"></div><div class="bar"><div class="fill" id="pf"></div></div>
<div id="l">{{range .Items}}<div class="c" id="c{{.ID}}"><div class="s">{{.Subject}}</div><div class="m">{{.Meta}}</div><div class="t">{{.Text}}</div>
<button id="b{{.ID}}" onclick="hit({{.ID}})">✔ Выполнено</button>{{if .Mine}}<button id="x{{.ID}}" onclick="del({{.ID}})">🗑 Удалить</button>{{end}}</div>
{{end}}</div>{{if .CanAdd}}<button onclick="add()">➕ Добавить ДЗ</button>{{end}}<div id="e"></div>
<script>` + widgetJS + `
var IDS=[{{range .Items}}{{.ID}},{{end}}0],D={},ARM=0;
function paint(){var n=0,k=0;for(var i=0;i<IDS.length;i++){var id=IDS[i];if(!id)continue;var c=el('c'+id);if(!c||c.className.indexOf('gone')>=0)continue;n++;var d=!!D[id];if(d)k++;c.className=d?'c ok':'c';var b=el('b'+id);if(b)b.textContent=d?'↩ Вернуть':'✔ Выполнено'}
el('hd').textContent='Выполнено '+k+' из '+n;var f=el('pf');if(f)f.style.width=(n?Math.round(k*100/n):0)+'%'}
function apply(s){var j=jget(s);D={};var a=j.d||[];for(var i=0;i<a.length;i++)D[a[i]]=1;
if(j.n&&j.n.indexOf('del:')===0){var c=el('c'+j.n.slice(4));if(c)c.className='c gone'}paint()}
function hit(id){var b=el('b'+id);if(b)b.disabled=true;err('');req('d:'+id).then(function(s){apply(s);if(b)b.disabled=false},function(e){err('Ошибка: '+e);if(b)b.disabled=false})}
function del(id){if(ARM!==id){ARM=id;el('x'+id).textContent='Точно удалить?';return}ARM=0;req('x:'+id).then(apply,function(e){err('Ошибка: '+e)})}
function add(){req('a:').then(function(){err('Открой чат — пришлю форму')},function(e){err('Ошибка: '+e)})}
boot(apply);
</script>`)

func renderBoard(items []boardItem, canAdd bool) (string, error) {
	return render(tplBoard, struct {
		Items  []boardItem
		CanAdd bool
	}{items, canAdd})
}

// ---------------------------------------------------------------- 2. дашборд автора

var tplDash = newTpl(`<div class="h">📋 Мои задания</div>
<div id="l">{{range .Items}}<div class="c" id="c{{.ID}}"><div class="s">{{.Subject}}</div><div class="m">{{.Meta}}</div><div class="t">{{.Text}}</div>
<div class="m" id="s{{.ID}}">Сдали: {{.Done}} из {{.Total}}</div><div class="bar"><div class="fill" id="p{{.ID}}" style="width:{{.Pct}}%"></div></div>
<button id="x{{.ID}}" onclick="del({{.ID}})">🗑 Удалить</button></div>
{{end}}</div><button onclick="ref()">🔄 Обновить</button><div id="e"></div>
<script>` + widgetJS + `
var IDS=[{{range .Items}}{{.ID}},{{end}}0],ARM=0;
function bars(st){for(var i=0;i<IDS.length;i++){var id=IDS[i];if(!id)continue;var v=st[id];if(!v)continue;
var e=el('s'+id);if(e)e.textContent='Сдали: '+v[0]+' из '+v[1];var p=el('p'+id);if(p)p.style.width=(v[1]?Math.round(v[0]*100/v[1]):0)+'%'}}
function apply(s){var j=jget(s);if(j.s)bars(j.s);if(j.n&&j.n.indexOf('del:')===0){var c=el('c'+j.n.slice(4));if(c)c.className='c gone'}}
function del(id){if(ARM!==id){ARM=id;el('x'+id).textContent='Точно удалить?';return}ARM=0;req('x:'+id).then(apply,function(e){err('Ошибка: '+e)})}
function ref(){err('');req('r:').then(apply,function(e){err('Ошибка: '+e)})}
boot(apply);
</script>`)

func renderDash(items []dashItem) (string, error) {
	return render(tplDash, struct{ Items []dashItem }{items})
}

// ---------------------------------------------------------------- 3. профиль

var tplProfile = newTpl(`<div class="h">{{.Title}}</div>
<div class="c"><div class="s">{{.Name}}</div><div class="m">{{.Line1}}</div><div class="t">{{.Line2}}</div></div>
<button onclick="nav('hw')">📚 ДЗ</button><button onclick="nav('stats')">📊 Статистика</button><div id="e"></div>
<script>` + widgetJS + `
function nav(k){err('');req('n:'+k).then(function(){err('Отправил в чат')},function(e){err('Ошибка: '+e)})}
if(W){W.ready()}
</script>`)

func renderProfile(v profileView) (string, error) { return render(tplProfile, v) }

// ---------------------------------------------------------------- 4. статистика

var tplStats = newTpl(`<div class="h">📊 {{.Title}}</div>
{{range .Rows}}<div class="c"><div class="s">{{.Name}}</div><div class="m">{{.Done}} из {{.Total}}</div>
<div class="bar"><div class="fill" style="width:{{.Pct}}%"></div></div></div>{{end}}
<button onclick="ref()">🔄 Обновить</button><div id="e"></div>
<script>` + widgetJS + `
function ref(){err('');req('r:stats').then(function(){err('Свежая статистика — в чате')},function(e){err('Ошибка: '+e)})}
if(W){W.ready()}
</script>`)

func renderStats(title string, rows []statRow) (string, error) {
	return render(tplStats, struct {
		Title string
		Rows  []statRow
	}{title, rows})
}
