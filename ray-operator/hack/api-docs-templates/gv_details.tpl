{{- define "gvDetails" -}}
{{- $gv := . -}}

## {{ $gv.GroupVersionString }}

{{ $gv.Doc }}

{{- if $gv.Kinds  }}
### Resource Types
{{- range $gv.SortedKinds }}
- {{ template "typeLink" ($gv.TypeForKind .) }}
{{- end }}
{{ end }}

{{ range $gv.SortedTypes }}
{{ template "type" . }}
{{ end }}

{{- end -}}
