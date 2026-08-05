{{/*
  Helpers that make cross-references in the generated API reference unambiguous.

  crd-ref-docs' built-in Markdown renderer derives an anchor from the type name
  alone, so a type that exists in more than one API version (RayClusterSpec is
  defined in both ray.io/v1 and ray.io/v1alpha1, among others) produces the same
  anchor twice. Markdown renderers de-duplicate the heading ids but not the
  links, so every link in the ray.io/v1alpha1 section silently resolves to the
  ray.io/v1 definition of that type.

  We therefore emit an explicit, version-qualified anchor next to each type
  heading (`#rayclusterspec-v1alpha1`) and point every link at it. The
  auto-generated heading ids are left untouched, so existing deep links such as
  `#rayclusterspec` keep working.
*/}}

{{/* Version-qualified anchor for a locally defined type, e.g. "rayclusterspec-v1alpha1". */}}
{{- define "typeAnchor" -}}
{{- markdownSafeID (printf "%s-%s" .Name (last (splitList "/" .Package))) -}}
{{- end -}}

{{/*
  Same as markdownRenderTypeLink, except that links to locally defined types use
  the version-qualified anchor. External links (Kubernetes API docs and known
  types) and plain text are passed through unchanged.
*/}}
{{- define "typeLink" -}}
{{- $type := . -}}
{{- $rendered := markdownRenderTypeLink $type -}}
{{- if hasPrefix (printf "[%s](#" $type.Name) $rendered -}}
[{{ $type.Name }}](#{{ template "typeAnchor" $type }})
{{- else -}}
{{- $rendered -}}
{{- end -}}
{{- end -}}

{{/* Same as markdownRenderType, but routes every link through "typeLink". */}}
{{- define "typeRef" -}}
{{- $type := . -}}
{{- $kind := trimAll "\"" (toJson $type.Kind) -}}
{{- if eq $kind "MAP" -}}
object (keys:{{ template "typeLink" $type.KeyType }}, values:{{ template "typeLink" $type.ValueType }})
{{- else if eq $kind "SLICE" -}}
{{ template "typeLink" $type.UnderlyingType }} array
{{- else -}}
{{- template "typeLink" $type -}}
{{- end -}}
{{- end -}}
