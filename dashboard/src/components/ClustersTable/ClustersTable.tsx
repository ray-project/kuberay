import { useDeleteJobs } from "@/hooks/api/useDeleteJobs";
import { useListJobs } from "@/hooks/api/useListJobs";
import { JobRow, JobType, JobStatus } from "@/types/rayjob";
import { filterCluster } from "@/utils/filter";
import { Chip, IconButton } from "@mui/joy";
import dayjs from "dayjs";
import Image from "next/image";
import React from "react";
import RayIcon from "../../../public/ray.png";
import { FrontendTable } from "../FrontendTable/FrontendTable";
import { HeadCell } from "../FrontendTable/FrontendTableHead";
import { FrontendTableToolbar } from "../FrontendTable/FrontendTableToolbar";
import { ClusterRow, ClusterStatus } from "@/types/raycluster";
import { useListClusters } from "@/hooks/api/useListClusters";
import {
  getClusterStatusColor,
  getClusterStatusDisplay,
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
    null,
  );
  const [typeFilter, setTypeFilter] = React.useState<number>(0);
  const [refreshInterval, setRefreshInterval] = React.useState(5000);

  const { clusters, isLoading, error } = useListClusters(refreshInterval);
  const { deleting, deleteClusters } = useDeleteClusters();

  const filteredItems = React.useMemo(
    () => filterCluster(clusters, search, statusFilter, typeFilter),
    [clusters, search, statusFilter, typeFilter],
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
            {getClusterStatusDisplay(row.clusterState)}
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
                src={RayIcon}
                alt="Ray Dashboard"
                width={26}
                height={26}
              />
            </IconButton>
          )}
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
