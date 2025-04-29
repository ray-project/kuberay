"use client";

import {
  Alert,
  Box,
  Button,
  Divider,
  Link,
  Stack,
  ToggleButtonGroup,
  Typography,
} from "@mui/joy";
import NextLink from "next/link";
import WorkIcon from "@mui/icons-material/Work";
import LanIcon from "@mui/icons-material/Lan";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useFirstVisit } from "@/components/FirstVisitContext";
import { roblox } from "@/utils/constants";
const HomePage = () => {
  const router = useRouter();
  const { firstVisit } = useFirstVisit();
  useEffect(() => {
    if (firstVisit) {
      router.push("/jobs");
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
          Ray Platform
        </Typography>
      </Box>
      <Alert color="warning" sx={{ my: 3 }}>
        Excuse the mess. This is current under development.
      </Alert>
      <ToggleButtonGroup spacing={2} className="flex-wrap">
        <Button
          className="flex flex-col items-start justify-start py-3"
          component={NextLink}
          href="/jobs"
        >
          <Stack
            direction="row"
            width={200}
            gap={1}
            alignItems="center"
            marginBottom={0.7}
          >
            <WorkIcon />
            <Typography level="title-lg">Ray Jobs</Typography>
          </Stack>
          <Typography level="body-xs" textAlign="left" width={200}>
            Production jobs that tears the ephemeral cluster down when finished.
            Includes Batch API jobs.
          </Typography>
        </Button>
        <Button
          className="flex flex-col items-start justify-start py-3"
          component={NextLink}
          href="/clusters"
        >
          <Stack
            direction="row"
            width={200}
            gap={1}
            alignItems="center"
            marginBottom={0.7}
          >
            <LanIcon />
            <Typography level="title-lg">Ray Clusters</Typography>
          </Stack>
          <Typography level="body-xs" textAlign="left" width={200}>
            Persistent clusters for interactive development sessions. VS Code
            included.
          </Typography>
        </Button>
      </ToggleButtonGroup>
      {roblox && (
        <>
          <Divider sx={{ mt: 3 }} />
          <Typography level="h3" component="h3" sx={{ my: 2 }}>
            Documentation
          </Typography>
          <Box display="flex" gap={3}>
            <Link
              variant="outlined"
              color="neutral"
              borderRadius="sm"
              target="_blank"
              sx={{ "--Link-gap": "0.5rem", px: 1, py: 0.5 }}
            >
              Ray SDK
            </Link>
            <Link
              variant="outlined"
              color="neutral"
              borderRadius="sm"
              target="_blank"
              sx={{ "--Link-gap": "0.5rem", px: 1, py: 0.5 }}
            >
              Batch API
            </Link>
            <Link
              variant="outlined"
              color="neutral"
              borderRadius="sm"
              target="_blank"
              sx={{ "--Link-gap": "0.5rem", px: 1, py: 0.5 }}
            >
              Ray Swagger API
            </Link>
            <Link
              variant="outlined"
              color="neutral"
              borderRadius="sm"
              target="_blank"
              sx={{ "--Link-gap": "0.5rem", px: 1, py: 0.5 }}
            >
              Ray & Batch API Runbook
            </Link>
          </Box>
          <Box display="flex" gap={3} my={2}>
            <Link
              color="neutral"
              borderRadius="sm"
              target="_blank"
            >
              Ray API Walkthrough
            </Link>
            <Link
              color="neutral"
              borderRadius="sm"
              target="_blank"
            >
              Multimodal Batch API Walkthrough
            </Link>
          </Box>
        </>
      )}
    </>
  );
};

export default HomePage;
