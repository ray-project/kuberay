"use client";
import { useFirstVisit } from "@/components/FirstVisitContext";
import { JobsTable } from "@/components/JobsTable/JobsTable";
import { AddRounded } from "@mui/icons-material";
import { Box, Button, Typography } from "@mui/joy";
import NextLink from "next/link";
import { useEffect } from "react";

export default function JobsPage() {
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
          Jobs
        </Typography>
        <Button
          variant="outlined"
          color="primary"
          size="sm"
          startDecorator={<AddRounded />}
          component={NextLink}
          href="/jobs/new"
        >
          Create Job
        </Button>
      </Box>
      <JobsTable />
    </>
  );
}
