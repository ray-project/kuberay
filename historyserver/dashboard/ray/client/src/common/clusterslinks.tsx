import { Link, SxProps, Theme } from "@mui/material";
import React, { PropsWithChildren, useState } from "react";
import { Link as RouterLink } from "react-router-dom";
import { ClassNameProps } from "./props";
import { GlobalContext } from "../App";

type ClusterLinkProps = PropsWithChildren<
    {
        clusterName: string;
        clusterNamespace: string;
        sessionName: string;
        /**
         * This can be provided to override where we link to.
         */
        to?: string;
        sx?: SxProps<Theme>;
    } & ClassNameProps
>;
export const generateClusterLink = (clusterName: string, clusterNamespace: string, sessionName: string) => `/clusters/${clusterNamespace}/${clusterName}/${sessionName}`;

/**
 * A link to the top-level Cluster detail page.
 */
export const ClusterLink = ({
  clusterName,
  clusterNamespace,
  sessionName,
  to,
  children,
  className,
  sx,
}: ClusterLinkProps) => {
  return (
    <Link
      className={className}
      sx={sx}
      component={RouterLink}
      to={to ?? generateClusterLink(clusterName, clusterNamespace, sessionName)}
    >
      {children ?? clusterName}
    </Link>
  );
};

