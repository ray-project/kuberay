"use client";

import React from "react";
import {
  FormLabel,
  Stack,
  FormControl,
  Input,
  Typography,
  Divider,
  Box,
  Button,
  Stepper,
  Step,
  StepIndicator,
  ToggleButtonGroup,
  AccordionGroup,
  Accordion,
  AccordionSummary,
  AccordionDetails,
  accordionClasses,
  Select,
  Option,
  FormHelperText,
  Alert,
} from "@mui/joy";
import DeveloperBoardIcon from "@mui/icons-material/DeveloperBoard";
import StorageIcon from "@mui/icons-material/Storage";
import MemoryIcon from "@mui/icons-material/Memory";
import CreateIcon from "@mui/icons-material/Create";
import { InfoOutlined } from "@mui/icons-material";
import { mutate } from "swr";
import { useSnackBar } from "./SnackBarProvider";
import { useCreateJob } from "@/hooks/api/useCreateJob";

interface ComputeTemplateButton {
  value: string;
  icon: React.ReactNode;
  title: string;
  description: string;
}

const computeTemplateButtons: ComputeTemplateButton[] = [
  {
    value: "cpu",
    icon: <MemoryIcon />,
    title: "CPU",
    description: "For CPU-intensive workloads with 256 CPU cores. ",
  },
  {
    value: "memory",
    icon: <StorageIcon />,
    title: "Memory",
    description: "For memory-intensive workloads with 500 GiB memory. ",
  },
  {
    value: "gpu",
    icon: <DeveloperBoardIcon />,
    title: "GPU",
    description: "For GPU-accelerated workloads with 4 GPUs.",
  },
  {
    value: "custom",
    icon: <CreateIcon />,
    title: "Custom",
    description: "Create your own cluster compute template.",
  },
];

export const CreateJobForm = () => {
  const [jobName, setJobName] = React.useState(
    "test-job" + Math.floor(Math.random() * 100)
  );
  const [dockerImage, setDockerImage] = React.useState("rayproject/ray:2.46.0");
  const [entrypoint, setEntrypoint] = React.useState(
    'python -c \\"import time; time.sleep(10)\\"'
  );
  const [computeTemplate, setComputeTemplate] = React.useState<string | null>(
    null
  );
  const [customComputeTemplate, setCustomComputeTemplate] =
    React.useState("new-template");
  const [newCustomComputeTemplate, setNewCustomComputeTemplate] =
    React.useState("");

  const { creating, createJob } = useCreateJob();

  const handleCreateJob = async () => {
    await createJob(jobName, dockerImage, entrypoint);
  };
  return (
    <>
      <Alert color="danger" sx={{ mb: 3 }}>
        This is current under development and not ready for use.
      </Alert>
      <Stepper
        orientation="vertical"
        sx={{
          "--Stepper-verticalGap": "1rem",
          "--Step-gap": ".5rem",
          "--StepIndicator-size": "1.7rem",
        }}
      >
        <Step
          indicator={
            <StepIndicator
              variant={jobName ? "solid" : "outlined"}
              color="primary"
            >
              1
            </StepIndicator>
          }
        >
          <Typography level="title-lg">Details</Typography>
          <FormControl size="sm">
            <FormLabel>Name</FormLabel>
            <Input
              placeholder="ex: my-rayjob"
              value={jobName}
              onChange={(e) => setJobName(e.target.value)}
            />
          </FormControl>
        </Step>
        <Step
          indicator={
            <StepIndicator
              variant={dockerImage && entrypoint ? "solid" : "outlined"}
              color="primary"
            >
              2
            </StepIndicator>
          }
        >
          <Typography level="title-lg">Environment</Typography>
          <Box sx={{ mb: 1 }}>
            <Typography level="body-sm" sx={{ mt: -1, mb: 1 }}>
              Docker image with your code and packages
            </Typography>
            <FormControl
              size="sm"
              sx={{ mb: 1.5 }}
              error={dockerImage.startsWith("http")}
            >
              <FormLabel>Docker Image</FormLabel>
              <Input
                placeholder="ex: docker.artifactory.rbx.com/your-image-with-code"
                value={dockerImage}
                onChange={(e) => setDockerImage(e.target.value)}
              />
              {dockerImage.startsWith("http") && (
                <FormHelperText>
                  <InfoOutlined />
                  Docker image should not start with http.
                </FormHelperText>
              )}
            </FormControl>
            <FormControl size="sm">
              <FormLabel>Entrypoint</FormLabel>
              <Input
                placeholder="ex: python /path/to/driver.py"
                value={entrypoint}
                onChange={(e) => setEntrypoint(e.target.value)}
              />
            </FormControl>
          </Box>
        </Step>
        <Step
          indicator={
            <StepIndicator
              variant={computeTemplate ? "solid" : "outlined"}
              color="primary"
            >
              3
            </StepIndicator>
          }
        >
          <Typography level="title-lg">Compute Template</Typography>
          <Typography level="body-sm" sx={{ mt: -1, mb: 1 }}>
            Hardware resources for your Ray cluster
          </Typography>
          <ToggleButtonGroup
            value={computeTemplate}
            spacing={2}
            onChange={(e, newValue) => {
              setComputeTemplate(newValue);
            }}
            className="flex-wrap"
          >
            {computeTemplateButtons.map((button) => (
              <Button
                className="flex flex-col items-start justify-start py-3"
                value={button.value}
                key={button.value}
              >
                <Stack
                  direction="row"
                  width={120}
                  gap={1}
                  alignItems="center"
                  marginBottom={0.7}
                >
                  {button.icon}
                  <Typography level="title-md">{button.title}</Typography>
                </Stack>
                <Typography level="body-xs" textAlign="left" width={120}>
                  {button.description}
                </Typography>
              </Button>
            ))}
          </ToggleButtonGroup>
          {computeTemplate === "custom" && (
            <Stack direction="row" gap={2} mt={2}>
              <FormControl sx={{ flex: 1 }}>
                <FormLabel>Choose template: </FormLabel>
                <Select
                  value={customComputeTemplate}
                  onChange={(e, newValue) =>
                    setCustomComputeTemplate(newValue!)
                  }
                >
                  <Option value="new-template">Create new template</Option>
                  <Option value="default-template">default-template</Option>
                </Select>
              </FormControl>
              {customComputeTemplate === "new-template" && (
                <FormControl sx={{ flex: 1 }}>
                  <FormLabel>New template name:</FormLabel>
                  <Input placeholder="new-template-name" />
                </FormControl>
              )}
            </Stack>
          )}
          {/* <Divider sx={{ mt: 2 }} /> */}
          <AccordionGroup
            disableDivider
            sx={{
              mt: 2,
              [`& .${accordionClasses.root}`]: {
                "& button": {
                  borderRadius: "5px",
                },
              },
            }}
          >
            <Accordion sx={{ mb: 1 }}>
              <AccordionSummary>Head Node</AccordionSummary>
              <AccordionDetails sx={{ mt: 0.5 }}>
                <Divider />
                <Typography level="title-md" mt={1.5}>
                  Requests
                </Typography>
                <Stack direction="row" gap={2} mt={0.5}>
                  <FormControl size="sm">
                    <FormLabel>CPU Cores</FormLabel>
                    <Input value="2" />
                  </FormControl>
                  <FormControl size="sm">
                    <FormLabel>Memory</FormLabel>
                    <Input value="4" />
                  </FormControl>
                  <FormControl size="sm">
                    <FormLabel>GPU</FormLabel>
                    <Input value="0" />
                  </FormControl>
                </Stack>
                <Typography level="title-md" mt={1.5}>
                  Limits
                </Typography>
                <Stack direction="row" gap={2} mt={0.5} mb={2}>
                  <FormControl size="sm">
                    <FormLabel>CPU Cores</FormLabel>
                    <Input value="4" />
                  </FormControl>
                  <FormControl size="sm">
                    <FormLabel>Memory</FormLabel>
                    <Input value="5" />
                  </FormControl>
                </Stack>
              </AccordionDetails>
            </Accordion>
            <Accordion>
              <AccordionSummary>Worker Nodes</AccordionSummary>
              <AccordionDetails sx={{ mt: 0.5 }}>
                <Divider />
                <Typography level="title-md" mt={1.5}>
                  Replicas
                </Typography>
                <Stack direction="row" gap={2} mt={0.5}>
                  <FormControl size="sm">
                    <FormLabel>Desired replicas</FormLabel>
                    <Input value="1" />
                  </FormControl>
                  <FormControl size="sm">
                    <FormLabel>Min replicas</FormLabel>
                    <Input value="0" />
                  </FormControl>
                  <FormControl size="sm">
                    <FormLabel>Max replicas</FormLabel>
                    <Input value="2" />
                  </FormControl>
                </Stack>
                <Typography level="title-md" mt={1.5}>
                  Requests
                </Typography>
                <Stack direction="row" gap={2} mt={0.5}>
                  <FormControl size="sm">
                    <FormLabel>CPU Cores</FormLabel>
                    <Input value="2" />
                  </FormControl>
                  <FormControl size="sm">
                    <FormLabel>Memory</FormLabel>
                    <Input value="4" />
                  </FormControl>
                  <FormControl size="sm">
                    <FormLabel>GPU</FormLabel>
                    <Input value="0" />
                  </FormControl>
                </Stack>
                <Typography level="title-md" mt={1.5}>
                  Limits
                </Typography>
                <Stack direction="row" gap={2} mt={0.5} mb={2}>
                  <FormControl size="sm">
                    <FormLabel>CPU Cores</FormLabel>
                    <Input value="4" />
                  </FormControl>
                  <FormControl size="sm">
                    <FormLabel>Memory</FormLabel>
                    <Input value="5" />
                  </FormControl>
                </Stack>
              </AccordionDetails>
            </Accordion>
          </AccordionGroup>
        </Step>
      </Stepper>
      <Stack direction="row" gap={2} mt={5} ml={4.5}>
        <Button
          className={"bg-[#0B6BCB]"}
          disabled={
            !(jobName && dockerImage && computeTemplate && computeTemplate)
          }
          onClick={handleCreateJob}
          loading={creating}
        >
          Create Job
        </Button>
        <Button variant="outlined" color="neutral">
          Cancel
        </Button>
      </Stack>
    </>
  );
};
