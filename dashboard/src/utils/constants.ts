export const ALL_NAMESPACES = "all"
// I'm developing in the stage cluster, so I'm directly using the deployed backend
const development = {
  url: "http://localhost:31888/apis/v1",
};

const production = {
  url: "http://localhost:31888/apis/v1",
};

export const config =
  process.env.NODE_ENV === "development" ? development : production;

export const roblox = false;
