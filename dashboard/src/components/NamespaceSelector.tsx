"use client";

import { FormControl, FormLabel, Option, Select } from "@mui/joy";
import React from "react";
import { useNamespace, useSetNamespace } from "@/components/NamespaceProvider";
import { useListNamespaces } from "@/hooks/api/useListNamespaces";
import { ALL_NAMESPACES } from "@/utils/config-defaults";

// Global namespace switcher. "All namespaces" lists resources from every
// namespace; operations always use the namespace of the targeted resource.
export const NamespaceSelector = () => {
  const namespace = useNamespace();
  const setNamespace = useSetNamespace();
  const { namespaces } = useListNamespaces();

  // Always offer "all" and keep the current namespace selectable even when the
  // namespace list fails to load (e.g. missing RBAC or a v1 apiserver).
  const options = Array.from(
    new Set([ALL_NAMESPACES, namespace, ...namespaces]),
  );

  return (
    <FormControl size="sm">
      <FormLabel>Namespace</FormLabel>
      <Select
        size="sm"
        value={namespace}
        slotProps={{ button: { sx: { whiteSpace: "nowrap" } } }}
        onChange={(_, newValue) => {
          if (newValue !== null) {
            setNamespace(newValue);
          }
        }}
      >
        {options.map((ns) => (
          <Option key={ns} value={ns}>
            {ns === ALL_NAMESPACES ? "All namespaces" : ns}
          </Option>
        ))}
      </Select>
    </FormControl>
  );
};
