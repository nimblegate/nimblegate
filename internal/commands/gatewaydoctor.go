// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net"
	"os"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gwicons"
	"nimblegate/internal/version"
)

// defaultAuthorizedKeysPath is the gateway's SSH authorized_keys file, shared by
// the CLI doctor flag default and the /health diagnostics tab.
const defaultAuthorizedKeysPath = "/srv/gateway/ssh/authorized_keys"

func gatewayDoctor(args []string) int {
	fs := flag.NewFlagSet("gateway doctor", flag.ExitOnError)
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "per-repo config dir root")
	reposRoot := fs.String("repos-root", "/srv/nimblegate-gateway/repos", "bare-repo root")
	authKeys := fs.String("ssh-authorized-keys", defaultAuthorizedKeysPath, "path to the SSH authorized_keys file")
	repo := fs.String("repo", "", "limit per-repo checks to a single repo")
	offline := fs.Bool("offline", false, "skip network checks (SSH gate dial + upstream auth)")
	jsonOut := fs.Bool("json", false, "emit the report as JSON")
	host := fs.String("host", "", "gateway reachable host for connect URLs (default: placeholder)")
	gatePort := fs.Int("gate-port", 0, "SSH gate port to probe (0 = probe 2222 then 22)")
	_ = fs.Parse(args)

	var gatePorts []int
	if *gatePort != 0 {
		gatePorts = []int{*gatePort}
	}
	rep := gateway.RunDoctor(gateway.DoctorConfig{
		PolicyRoot:         *policyRoot,
		ReposRoot:          *reposRoot,
		AuthorizedKeysPath: *authKeys,
		Host:               *host,
		Version:            version.Resolved(),
		RepoFilter:         *repo,
		Offline:            *offline,
		GatePorts:          gatePorts,
	})

	if *jsonOut {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "gateway doctor: %v\n", err)
			return 1
		}
		fmt.Println(string(b))
	} else {
		renderDoctorText(os.Stdout, rep)
	}
	if rep.HasFail {
		return 1
	}
	return 0
}

func renderDoctorText(w io.Writer, rep gateway.DoctorReport) {
	fmt.Fprintln(w, "Gateway diagnostics")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Global")
	for _, c := range rep.Checks {
		if c.Repo == "" {
			printDoctorLine(w, c)
		}
	}
	for _, name := range doctorRepoOrder(rep) {
		fmt.Fprintf(w, "\nRepo: %s\n", name)
		for _, c := range rep.Checks {
			if c.Repo == name {
				printDoctorLine(w, c)
			}
		}
	}
	if len(rep.Keys) > 0 {
		fmt.Fprintln(w, "\nAuthorized keys")
		for _, k := range rep.Keys {
			label := k.Comment
			if label == "" {
				label = "(no label)"
			}
			fmt.Fprintf(w, "  %s  %s  %s\n", k.Fingerprint, k.Type, label)
		}
	}
	for _, rc := range rep.Repos {
		fmt.Fprintf(w, "\nConnect a dev box / agent - %s\n", rc.Name)
		for i, step := range doctorConnectSteps(rep.Host, rc.Name) {
			fmt.Fprintf(w, "  %d. %s\n", i+1, step.Note)
			for _, cmd := range step.Cmds {
				fmt.Fprintf(w, "       %s\n", cmd)
			}
		}
	}
}

func printDoctorLine(w io.Writer, c gateway.DoctorCheck) {
	fmt.Fprintf(w, "[%s] %s - %s\n", c.Status, c.Name, c.Reason)
	if c.Fix != "" {
		fmt.Fprintf(w, "      fix: %s\n", c.Fix)
	}
}

func doctorRepoOrder(rep gateway.DoctorReport) []string {
	var order []string
	seen := map[string]bool{}
	for _, c := range rep.Checks {
		if c.Repo == "" || seen[c.Repo] {
			continue
		}
		seen[c.Repo] = true
		order = append(order, c.Repo)
	}
	return order
}

// doctorConnectStep is one copy-paste onboarding step: a note line plus zero or
// more shell commands to run on the dev box.
type doctorConnectStep struct {
	Note string
	Cmds []string
}

func doctorConnectSteps(host, repo string) []doctorConnectStep {
	pushURL := "ssh://git@" + host + ":2222/~/" + repo + ".git"
	return []doctorConnectStep{
		{
			Note: "On your dev box, print your key + fingerprint (your key may not be the default; `ls ~/.ssh/*.pub` to find it, or `ssh-keygen -t ed25519` to create one):",
			Cmds: []string{"cat ~/.ssh/id_ed25519.pub", "ssh-keygen -lf ~/.ssh/id_ed25519.pub"},
		},
		{
			Note: "Compare that fingerprint to the authorized keys above. Not listed? Add it at /ssh-keys.",
		},
		{
			Note: "Point origin at the gateway (substitute <host> with the gateway's reachable IP/hostname; the value shown is the address you reached this dashboard on):",
			Cmds: []string{"git remote set-url origin " + pushURL},
		},
		{
			Note: "Confirm auth + repo from your dev box:",
			Cmds: []string{"git ls-remote " + pushURL},
		},
	}
}

// healthTabStrip renders the Status / Diagnostics sub-tab strip for /health,
// mirroring settingsTabStrip.
func healthTabStrip(active string) string {
	cls := func(t string) string {
		if t == active {
			return "autopr-tab active"
		}
		return "autopr-tab"
	}
	return `<style>
.autopr-tabs{display:flex;gap:2px;margin:18px 0;border-bottom:1px solid var(--gw-border);padding:0}
.autopr-tab{display:inline-block;padding:10px 18px;color:var(--gw-text-muted);text-decoration:none;font-size:13px;font-weight:500;border-bottom:2px solid transparent;margin-bottom:-1px}
.autopr-tab:hover{color:var(--gw-text)}
.autopr-tab.active{color:var(--gw-accent);border-bottom-color:var(--gw-accent);font-weight:600}
</style>
<nav class="autopr-tabs">
<a href="/health?tab=status" class="` + cls("status") + `">Status</a>
<a href="/health?tab=diagnostics" class="` + cls("diagnostics") + `">Diagnostics</a>
</nav>`
}

type doctorCheckVM struct {
	Class  string
	Icon   string
	Name   string
	Reason string
	Fix    string
}

type doctorRepoVM struct {
	Name   string
	Checks []doctorCheckVM
	Conn   bool
	Steps  []doctorConnectStep
}

type doctorPageVM struct {
	Global []doctorCheckVM
	Repos  []doctorRepoVM
	Keys   []gateway.DoctorKey
	Online bool
}

func doctorStatusClassIcon(s gateway.DoctorStatus) (string, string) {
	switch s {
	case gateway.DoctorOK:
		return "gw-doc-ok", "ok"
	case gateway.DoctorWarn:
		return "gw-doc-warn", "warn"
	case gateway.DoctorFail:
		return "gw-doc-fail", "reject"
	default:
		return "gw-doc-info", "pending"
	}
}

// renderHealthDiagnostics runs the doctor engine and renders the /health
// Diagnostics tab body. rawHost is r.Host (port stripped here). The live
// connectivity checks (SSH gate dial + upstream auth) run only when online is
// set, so a normal page view stays fast and never hangs on a slow upstream.
func renderHealthDiagnostics(policyRoot, reposRoot, sshKeysPath, rawHost string, online bool) template.HTML {
	if sshKeysPath == "" {
		sshKeysPath = defaultAuthorizedKeysPath
	}
	rep := gateway.RunDoctor(gateway.DoctorConfig{
		PolicyRoot:         policyRoot,
		ReposRoot:          reposRoot,
		AuthorizedKeysPath: sshKeysPath,
		Host:               hostNoPort(rawHost),
		Version:            version.Resolved(),
		Offline:            !online,
	})

	vm := doctorPageVM{Keys: rep.Keys, Online: online}
	for _, c := range rep.Checks {
		if c.Repo != "" {
			continue
		}
		cl, ic := doctorStatusClassIcon(c.Status)
		vm.Global = append(vm.Global, doctorCheckVM{Class: cl, Icon: ic, Name: c.Name, Reason: c.Reason, Fix: c.Fix})
	}
	conn := map[string]bool{}
	for _, rc := range rep.Repos {
		conn[rc.Name] = true
	}
	for _, name := range doctorRepoOrder(rep) {
		rv := doctorRepoVM{Name: name}
		for _, c := range rep.Checks {
			if c.Repo != name {
				continue
			}
			cl, ic := doctorStatusClassIcon(c.Status)
			rv.Checks = append(rv.Checks, doctorCheckVM{Class: cl, Icon: ic, Name: c.Name, Reason: c.Reason, Fix: c.Fix})
		}
		if conn[name] {
			rv.Conn = true
			rv.Steps = doctorConnectSteps(rep.Host, name)
		}
		vm.Repos = append(vm.Repos, rv)
	}

	var buf bytes.Buffer
	if err := doctorTmpl.Execute(&buf, vm); err != nil {
		return template.HTML("<p class=\"sub\">diagnostics render error</p>")
	}
	return template.HTML(buf.String())
}

func hostNoPort(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}

var doctorTmpl = template.Must(template.New("doctor").Funcs(template.FuncMap{"icon": gwicons.HTML}).Parse(`<style>
.gw-doc-ok{color:#5ee68e}
.gw-doc-warn{color:#e6905e}
.gw-doc-fail{color:#e06c75}
.gw-doc-info{color:var(--gw-text-muted)}
.gw-doc-list{list-style:none;margin:6px 0;padding:0}
.gw-doc-list li{padding:5px 0;border-bottom:1px solid var(--gw-border-subtle);font-size:13px}
.gw-doc-list li:last-child{border-bottom:0}
.gw-doc-name{color:var(--gw-text);font-weight:600;margin-left:4px}
.gw-doc-reason{color:var(--gw-text-soft)}
.gw-doc-fix{display:block;margin:2px 0 0 22px;color:var(--gw-text-muted);font-size:12px}
.gw-doc-keys{list-style:none;margin:6px 0;padding:0;font-size:12px}
.gw-doc-keys li{padding:3px 0;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;color:var(--gw-text-soft)}
.gw-doc-steps{margin:6px 0;padding-left:20px;font-size:13px;color:var(--gw-text-soft)}
.gw-doc-steps li{margin:6px 0}
.gw-doc-steps pre{background:var(--gw-bg-control);padding:6px 10px;border-radius:4px;overflow-x:auto;font-size:12px;margin:4px 0 0}
</style>
<h2 class="gw-pagehead">Diagnostics</h2>
<p class="gw-pagedesc">Read-only preflight: global gateway health, per-repo policy, and a copy-paste block to connect a dev box. Nothing here changes state.</p>
{{if .Online}}<p class="sub">Live connectivity checks ran (SSH gate dial + upstream auth).</p>{{else}}<p class="sub">Config-only view for speed. <a href="/health?tab=diagnostics&amp;online=1" style="color:var(--gw-accent)">Run live connectivity checks</a> (SSH gate + upstream auth; may take a few seconds per repo).</p>{{end}}

<section class="frame">
<h3 class="gw-section-head">Global</h3>
<ul class="gw-doc-list">
{{range .Global}}<li><span class="{{.Class}}">{{icon .Icon}}</span><span class="gw-doc-name">{{.Name}}</span> <span class="gw-doc-reason">- {{.Reason}}</span>{{if .Fix}}<span class="gw-doc-fix">{{.Fix}}</span>{{end}}</li>
{{end}}</ul>
{{if .Keys}}<details><summary>Authorized keys ({{len .Keys}}) - manage at <a href="/ssh-keys" style="color:var(--gw-accent)">/ssh-keys</a></summary>
<ul class="gw-doc-keys">{{range .Keys}}<li>{{.Fingerprint}} {{.Type}} {{if .Comment}}{{.Comment}}{{else}}(no label){{end}}</li>{{end}}</ul>
</details>{{else}}<p class="sub">No authorized keys. Add one at <a href="/ssh-keys" style="color:var(--gw-accent)">/ssh-keys</a>.</p>{{end}}
</section>

{{range .Repos}}
<section class="frame">
<h3 class="gw-section-head">Repo: {{.Name}}</h3>
<ul class="gw-doc-list">
{{range .Checks}}<li><span class="{{.Class}}">{{icon .Icon}}</span><span class="gw-doc-name">{{.Name}}</span> <span class="gw-doc-reason">- {{.Reason}}</span>{{if .Fix}}<span class="gw-doc-fix">{{.Fix}}</span>{{end}}</li>
{{end}}</ul>
{{if .Conn}}<details><summary>Connect a dev box / agent</summary>
<ol class="gw-doc-steps">
{{range .Steps}}<li>{{.Note}}{{range .Cmds}}<pre>{{.}}</pre>{{end}}</li>
{{end}}</ol>
</details>{{end}}
</section>
{{end}}
`))
