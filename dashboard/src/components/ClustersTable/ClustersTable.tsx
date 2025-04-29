import { useDeleteJobs } from "@/hooks/api/useDeleteJobs";
import { useListJobs } from "@/hooks/api/useListJobs";
import { JobRow, JobType, Status } from "@/types/rayjob";
import { clusterIsNotebook, filterCluster, filterJobs } from "@/utils/filter";
import SubjectIcon from "@mui/icons-material/Subject";
import { Chip, IconButton, Tooltip } from "@mui/joy";
import dayjs from "dayjs";
import Image from "next/image";
import React from "react";
import GrafanaIcon from "../../../public/GrafanaIcon.svg";
import { FrontendTable } from "../FrontendTable/FrontendTable";
import { HeadCell } from "../FrontendTable/FrontendTableHead";
import { FrontendTableToolbar } from "../FrontendTable/FrontendTableToolbar";
import { ResourceQuotaAlert } from "../ResourceQuotaAlert";
import { ClusterRow, ClusterStatus } from "@/types/raycluster";
import { useListClusters } from "@/hooks/api/useListClusters";
import {
  getClusterStatus,
  getClusterStatusColor,
  getClusterStatusIcon,
} from "./ClusterStatusParser";
import { useDeleteClusters } from "@/hooks/api/useDeleteClusters";

const headCells: readonly HeadCell<ClusterRow>[] = [
  {
    id: "name",
    label: "Name",
    width: 200,
    sortable: true,
  },
  {
    id: "clusterState",
    label: "Status",
    width: 100,
    sortable: false,
  },
  {
    id: "createdAt",
    label: "Created At",
    width: 130,
    sortable: true,
  },
  {
    id: "links",
    label: "Links",
    width: 100,
    sortable: false,
  },
];

export const ClustersTable = () => {
  const [search, setSearch] = React.useState("");
  const [statusFilter, setStatusFilter] = React.useState<ClusterStatus | null>(
    null
  );
  const [typeFilter, setTypeFilter] = React.useState<number>(0);
  const [refreshInterval, setRefreshInterval] = React.useState(5000);

  const { clusters, isLoading, error } = useListClusters(refreshInterval);
  const { deleting, deleteClusters } = useDeleteClusters();

  const filteredItems = React.useMemo(
    () => filterCluster(clusters, search, statusFilter, typeFilter),
    [clusters, search, statusFilter, typeFilter]
  );

  const renderRow = (row: ClusterRow) => {
    return (
      <>
        <td>
          <Chip
            variant="soft"
            size="sm"
            startDecorator={getClusterStatusIcon(row.clusterState)}
            color={getClusterStatusColor(row.clusterState)}
          >
            {getClusterStatus(row.clusterState)}
          </Chip>
        </td>
        <td>{dayjs(row.createdAt).format("M/D/YY HH:mm:ss")}</td>
        <td className="flex">
          {row.clusterState === "READY" && (
            <IconButton
              variant="plain"
              size="sm"
              sx={{ minHeight: "1rem", minWidth: "1rem" }}
              title="Ray Dashboard"
              href={row.links.rayHeadDashboardLink}
              target="_blank"
              component="a"
            >
              <Image
                priority
                src="data:image/png;base64,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"
                alt="Ray Dashboard"
                width={26}
                height={26}
              />
            </IconButton>
          )}
          {
            row.clusterState === "READY" && row.links.notebookLink && (
              <IconButton
                variant="plain"
                size="sm"
                sx={{
                  minHeight: "1rem",
                  minWidth: "1rem",
                  px: 0.6,
                }}
                title="Loki Logs"
                href={row.links.notebookLink}
                target="_blank"
                component="a"
              >
                <Image
                  priority
                  src="data:image/png;base64,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"
                  alt="Ray Dashboard"
                  width={22}
                  height={22}
                />
              </IconButton>)
          }
          <IconButton
            variant="plain"
            size="sm"
            sx={{
              minHeight: "1rem",
              minWidth: "1rem",
              px: 0.5,
            }}
            title="Grafana Metrics"
            href={row.links.rayGrafanaDashboardLink}
            target="_blank"
            component="a"
          >
            <Image priority src={GrafanaIcon} alt="Grafana Metrics" />
          </IconButton>
        </td>
      </>
    );
  };

  return (
    <>
      {/* <ResourceQuotaAlert jobs={jobs} /> */}
      <FrontendTableToolbar
        setSearch={setSearch}
        statusFilter={statusFilter}
        setStatusFilter={setStatusFilter}
        statuses={["READY", "PENDING"]}
        refreshInterval={refreshInterval}
        setRefreshInterval={setRefreshInterval}
        name="clusters"
        typeFilter={typeFilter}
        setTypeFilter={setTypeFilter}
        types={["All", "Cluster", "Job"]}
      />
      <FrontendTable<ClusterRow>
        data={filteredItems}
        isLoading={isLoading}
        error={error}
        headCells={headCells}
        deleteItems={deleteClusters}
        deleting={deleting}
        renderRow={renderRow}
        defaultOrderBy="createdAt"
        name="clusters"
      />
    </>
  );
};
