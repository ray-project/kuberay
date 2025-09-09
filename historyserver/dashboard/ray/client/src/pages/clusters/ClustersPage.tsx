import {
    Box,
    Button,
    ButtonGroup,
    Grid, IconButton, Link,
    Switch,
    Table, TableBody,
    TableCell,
    TableContainer,
    TableHead,
    TableRow, Tooltip, Typography,
} from "@mui/material";
import Pagination from "@mui/material/Pagination";
import React from "react";
import {RiArrowDownSLine, RiArrowRightSLine} from "react-icons/ri";
import {Link as RouterLink, Outlet} from "react-router-dom";
import {CodeDialogButtonWithPreview} from "../../common/CodeDialogButton";
import {ClusterLink} from "../../common/clusterslinks";
import Loading from "../../components/Loading";
import { MetadataSection } from "../../components/MetadataSection";
import PercentageBar from "../../components/PercentageBar";
import {SearchInput, SearchSelect} from "../../components/SearchComponent";
import StateCounter from "../../components/StatesCounter";
import { StatusChip } from "../../components/StatusChip";
import TitleCard from "../../components/TitleCard";
import {HelpInfo} from "../../components/Tooltip";
import {NodeDetail} from "../../type/node";
import {memoryConverter} from "../../util/converter";
import { MainNavPageInfo } from "../layout/mainNavContext";
import {NodeCard} from "../node";
import {NodeGPUView} from "../node/GPUColumn";
import {NodeGRAM} from "../node/GRAMColumn";
import {NodeRows} from "../node/NodeRow";
import {useClusterList} from "./hook/useClusterList";

const TEXT_COL_MIN_WIDTH = 100;


type Cluster = {
    name: string;
    createTime: string;
    sessionName: string;
}


export const ClusterRow = ( {name, createTime, sessionName}:Cluster) => {

    return (
        <TableRow>
            <TableCell >
                {/*<Box minWidth={TEXT_COL_MIN_WIDTH}>{name}</Box> */}
                <Tooltip title={name} arrow>
                    <div>
                        <ClusterLink
                            clusterName={name}
                            sessionName={sessionName}
                        />
                    </div>
                </Tooltip>
            </TableCell>
            <TableCell>
                <Box minWidth={TEXT_COL_MIN_WIDTH}>{createTime}</Box>
            </TableCell>
        </TableRow>
    );
};

const columns = [
    {
        label: "ClusterName",
        helpInfo: (
            <Typography>
                RayClusterName
                <br />
            </Typography>
        ),
    },
    {
        label: "createTime",
        helpInfo: (
            <Typography>
                RayCluster CreateTime. <br />
            </Typography>
        ),
    },
];

export const ClustersPage = () => {

    const {clustersList} = useClusterList()

    return (
        <React.Fragment>
            <Box
                sx={{
                    padding: 2,
                    width: "100%",
                    position: "relative",
                }}
            >
                <TitleCard title="Cluster List">
                    {/*
                    <Grid container alignItems="center">
                        <Grid item>
                            <SearchInput
                                label="ClusterName"
                                //onChange={(value) => changeFilter("hostname", value.trim())}
                            />
                        </Grid>
                        <Grid item>
                            <SearchInput
                                label="CreateTime"
                                //onChange={(value) => setPage("pageSize", Math.min(Number(value), 500) || 10)}
                            />
                        </Grid>
                    </Grid>
                    */}
                    <div>
                    </div>
                        <TableContainer>
                            <Table>
                                <TableHead>
                                    <TableRow>
                                        {columns.map(({ label, helpInfo }) => (
                                            <TableCell key={label}>
                                                <Box
                                                    display="flex"
                                                    //justifyContent="center"
                                                    //alignItems="center"
                                                >
                                                    {label}
                                                    {helpInfo && (
                                                        <HelpInfo sx={{ marginLeft: 1 }}>{helpInfo}</HelpInfo>
                                                    )}
                                                </Box>
                                            </TableCell>
                                        ))}
                                    </TableRow>
                                </TableHead>
                                <TableBody>

                                    {clustersList.map((cluster) => (
                                        <ClusterRow  name={cluster.name} createTime={cluster.createTime} sessionName={cluster.sessionName} />
                                    ))}

                                </TableBody>
                            </Table>
                        </TableContainer>
                </TitleCard>
            </Box>

        </React.Fragment>
    );
};
