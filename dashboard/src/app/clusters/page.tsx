"use client";
import { ClustersTable } from "@/components/ClustersTable/ClustersTable";
import { AddRounded } from "@mui/icons-material";
import { Box, Button, Typography } from "@mui/joy";
import NextLink from "next/link";
import Tabs from "@mui/joy/Tabs";
import TabList from "@mui/joy/TabList";
import Tab, { tabClasses } from "@mui/joy/Tab";
import { useEffect } from "react";
import { useFirstVisit } from "@/components/FirstVisitContext";

export default function ClustersPage() {
  const { firstVisit, setFirstVisit} = useFirstVisit();
  useEffect(() => {
    if (firstVisit) {
      setFirstVisit(false);
    }
  }, [])
  return (
    <>
      <Box
        sx={{
          display: "flex",
          my: 0.5,
          gap: 1,
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <Typography level="h2" component="h2">
          Clusters
        </Typography>
        <Box sx={{ display: "flex" }}>
          {/* <Button
            variant="outlined"
            color="primary"
            size="sm"
            startDecorator={<AddRounded />}
            component={NextLink}
            href="/clusters/new"
          >
            Create Cluster
          </Button> */}
        </Box>
      </Box>
      <ClustersTable />
    </>
  );
}
