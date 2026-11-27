package gen

import (
	"bytes"
	"fmt"
	"strings"
)

func proxyFile(o Options, s Service) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(Header)
	fmt.Fprintf(&b, "// Package proxy is %s.Service over NATS (forge 21a-2): each call goes to\n// a server of the %s service, wherever it runs.\npackage proxy\n\n", s.Pkg, s.Name)
	b.WriteString("import (\n")
	fmt.Fprintf(&b, "\t%q\n\t%q\n", o.Services, s.Import)
	for _, e := range s.Embedded {
		fmt.Fprintf(&b, "\t%s %q\n", e.Alias, e.Pkg)
	}
	b.WriteString(")\n\n")
	fmt.Fprintf(&b, "// Service is the %s service's name on the wire.\nconst Service = %q\n\n", s.Name, s.Name)
	b.WriteString("// Proxy sends each call to a server of the service.\ntype Proxy struct {\n\tC *services.Client\n")
	fmt.Fprintf(&b, "\t// Local, when set, runs the operations that cannot be sent (local and\n\t// streamed ones) in this process.\n\tLocal %s.Service\n", s.Pkg)
	for _, e := range s.Embedded {
		fmt.Fprintf(&b, "\t// %s.%s cannot be sent as it is: the caller sets it.\n\t%s.%s\n", e.Alias, e.Name, e.Alias, e.Name)
	}
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "var _ %s.Service = (*Proxy)(nil)\n\n", s.Pkg)
	b.WriteString("// New is a proxy sending its calls with c.\nfunc New(c *services.Client) *Proxy { return &Proxy{C: c} }\n")
	for _, op := range s.Ops {
		res := qualify(s.Pkg, op.Result)
		fmt.Fprintf(&b, "\nfunc (p *Proxy) %s(args %s.%s)", op.Name, s.Pkg, op.Args)
		if res != "" {
			b.WriteString(" " + res)
		}
		b.WriteString(" {\n")
		if op.Local || op.Stream {
			ret := "return "
			if res == "" {
				ret = ""
			}
			fmt.Fprintf(&b, "\tif p.Local != nil {\n\t\t%sp.Local.%s(args)\n", ret, op.Name)
			if res == "" {
				b.WriteString("\t\treturn\n")
			}
			b.WriteString("\t}\n")
		}
		switch {
		case op.Local && res == "":
			fmt.Fprintf(&b, "\tservices.Refuse(args, Service, %q)\n", op.Name)
		case op.Local:
			fmt.Fprintf(&b, "\treturn services.NotServed[%s](Service, %q, services.CodeLocal)\n", res, op.Name)
		case op.Stream:
			fmt.Fprintf(&b, "\treturn services.NotServed[%s](Service, %q, services.CodeStream)\n", res, op.Name)
		default:
			opts := ""
			if op.Idempotent {
				opts = ", services.Idempotent()"
			}
			fmt.Fprintf(&b, "\treturn services.Call[%s](p.C, Service, %q, args%s)\n", res, op.Name, opts)
		}
		b.WriteString("}\n")
	}
	return formatted(s.Dir+"/proxy/proxy.go", b.Bytes())
}

func serverFile(o Options, s Service) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(Header)
	fmt.Fprintf(&b, "// Package server serves %s.Service's calls from NATS (forge 21a-2),\n// handing each to a real implementation.\npackage server\n\n", s.Pkg)
	fmt.Fprintf(&b, "import (\n\t%q\n\t%q\n\t%q\n)\n\n", o.Services, s.Import, "github.com/nats-io/nats.go")
	fmt.Fprintf(&b, "// Ops are the operations a server runs; local and streamed ones are\n// left out.\nfunc Ops(svc %s.Service) map[string]services.Handler {\n\treturn map[string]services.Handler{\n", s.Pkg)
	for _, op := range s.Ops {
		if op.Local || op.Stream {
			continue
		}
		fmt.Fprintf(&b, "\t\t%q: services.Unary(svc.%s),\n", op.Name, op.Name)
	}
	b.WriteString("\t}\n}\n\n")
	fmt.Fprintf(&b, "// Serve serves svc's calls on nc.\nfunc Serve(nc *nats.Conn, svc %s.Service, opts ...services.ServerOption) (*services.Server, error) {\n\treturn services.Serve(nc, %q, Ops(svc), opts...)\n}\n", s.Pkg, s.Name)
	return formatted(s.Dir+"/server/server.go", b.Bytes())
}

func catalogFile(o Options, svcs []Service) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(Header)
	b.WriteString("// Package catalog lists every service of the module with its operations,\n// proxy and server (forge 21a-2).\npackage catalog\n\n")
	b.WriteString("import (\n\t\"reflect\"\n\n\t\"github.com/nats-io/nats.go\"\n")
	fmt.Fprintf(&b, "\t%q\n", o.Services)
	for _, s := range svcs {
		fmt.Fprintf(&b, "\t%q\n\t%sproxy %q\n\t%sserver %q\n", s.Import, s.Name, s.Import+"/proxy", s.Name, s.Import+"/server")
	}
	b.WriteString(")\n\n// Services are the module's services, by wire name.\nvar Services = []services.Spec{\n")
	for _, s := range svcs {
		fmt.Fprintf(&b, "\t{\n\t\tName: %q,\n\t\tInterface: reflect.TypeOf((*%s.Service)(nil)).Elem(),\n\t\tOps: []services.OpSpec{\n", s.Name, s.Pkg)
		for _, op := range s.Ops {
			var flags []string
			for _, f := range []struct {
				on   bool
				name string
			}{{op.Idempotent, "Idempotent"}, {op.Local, "Local"}, {op.Stream, "Stream"}, {op.Job, "Job"}} {
				if f.on {
					flags = append(flags, f.name+": true")
				}
			}
			extra := ""
			if len(flags) > 0 {
				extra = ", " + strings.Join(flags, ", ")
			}
			fmt.Fprintf(&b, "\t\t\t{Name: %q%s},\n", op.Name, extra)
		}
		fmt.Fprintf(&b, "\t\t},\n\t\tProxy: func(c *services.Client) any { return %sproxy.New(c) },\n", s.Name)
		fmt.Fprintf(&b, "\t\tServe: func(nc *nats.Conn, svc any, opts ...services.ServerOption) (*services.Server, error) {\n\t\t\treturn %sserver.Serve(nc, svc.(%s.Service), opts...)\n\t\t},\n\t},\n", s.Name, s.Pkg)
	}
	b.WriteString("}\n")
	return formatted(o.Catalog+"/catalog.go", b.Bytes())
}

// qualify is a result type in the service's package: "X" → "pkg.X",
// "*X" → "*pkg.X".
func qualify(pkg, t string) string {
	switch {
	case t == "":
		return ""
	case strings.HasPrefix(t, "*"):
		return "*" + pkg + "." + t[1:]
	}
	return pkg + "." + t
}
