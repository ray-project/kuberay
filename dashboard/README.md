# Kuberay Dashboard

This is the repo for the open source dashboard for KubeRay

![image](https://github.com/user-attachments/assets/3c71169d-44c6-45ee-907d-b8a44043b861)

## How to deploy with KubeRay Operator and API Server

First, clone the KubeRay repo and cd to the `apiserver` folder. Then, create a local cluster and install the KubeRay
components by running

```bash
make cluster
make operator-image
make load-operator-image
make deploy-operator
make install
```

Now, you should be able to access the KubeRay apiserver at `http://localhost:31888/apis/v1/namespaces/default/jobs`.

Now, to deploy the dashboard, you can run `npm run dev` and go to `localhost:3000/ray/jobs` on your browser. Note that
you might need to disable CORS by launching Chrome with

```bash
open -n -a /Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome --args --user-data-dir="/tmp/chrome_dev_test"
--disable-web-security
```

If you have any ray jobs in the kind cluster, they will show up here. If not, you can create new ones by clicking on the
Create Job button, choosing a random compute template (they don't work yet), and clicking "Create Job".

If you run into an error about missing compute template, you can apply the demo/compute-template.yaml file.

## What works

You can view the list of ray jobs and ray clusters. You can search and filter them using frontend components. You can
delete them using the select button. You can also create a test job, but the compute templates don't work yet.

## What doesn't work

The Grafana and Logs link don't work since the KubeRay apiserver doesn't return the metrics/logs links. We should allow
users to customize their metrics/logs link templates based on their observability setup.

Create job doesn't work with compute templates yet. We don't have a detailed view of each job/cluster. We also don't
have links to the Ray head node dashboard as the user would need to expose them securely. We also need to add a
namespace selector since it's using the "default" namespace right now.

## Note on open source

There are currently roblox-only components that are hidden with a flag in the codebase. We want to remove them
eventually while keeping the roblox fork easily synced with the OSS branch. Any suggestions for this are welcome.

## Tech stack

### Language ➜ Typescript

Should be more maintainable than JS!

### Library ➜ React

We choose React as our javascript library since it’s the industry standard. It’s great for creating interactive Single
Page Applications (SPA).

### Framework ➜ Next.js

We need a framework to handle the tooling and configuration needed for React. Next.js has replaced create-react-app as
the standard React framework.

Since we don’t need SEO, we can build it as a SPA instead of using SSR (server-side rendering), which Next.js is known
for. Nonetheless, Next.js is still good for building SPAs, since it provides a file-system based router, fast local
development with TurboPack, a performant Rust-based transpiler called SWC, and built-in optimizations for asset-loading
(such as fonts and images).

To disable SSR, we can use React Router and change server config like
[this](https://dev.to/apkoponen/how-to-disable-server-side-rendering-ssr-in-next-js-1563) or use dynamic.

### UI Framework ➜ MUI (Joy UI)

Between Material UI, which has an older Google-designed look that is also used by Kubeflow, and NextUI, which is more
modern, we choose Joy UI, which sits somewhere in the middle. Joy UI is a modified version of MUI with a clean and
modern look that is not too out-of-place with the Kubeflow interface.

### Data Fetching ➜ SWR

While we can directly use JavaScript fetch() to fetch data, Vercel’s SWR hook is easier to use and more feature-rich.
SWR makes it easy to continuously fetch the latest data to keep the frontend up-to-date without manual refreshing, and
it also uses caching with the stale-while-revalidate strategy to provide a smooth user experience. In addition, SWR
simplifies our data-fetching code to be more maintainable.

Since our app is a dashboard, we want to revalidate the data every 5s like Grafana. Since we care more about accurate
information than UI latency, we won’t use the optimistic UI feature for POST requests, which mutates data immediately
and only rollback if the request didn’t go through. Instead, we will show a spinner for every API call like Kubeflow
does.

## Next.js

This is a [Next.js](https://nextjs.org/) project bootstrapped with
[`create-next-app`](https://github.com/vercel/next.js/tree/canary/packages/create-next-app).

### Getting Started

First, run the development server:

```bash
npm run dev
# or
yarn dev
# or
pnpm dev
# or
bun dev
```

Open [http://localhost:3000](http://localhost:3000) with your browser to see the result.

You can start editing the page by modifying `app/page.tsx`. The page auto-updates as you edit the file.

This project uses [`next/font`](https://nextjs.org/docs/basic-features/font-optimization) to automatically optimize and
load Inter, a custom Google Font.

### Learn More

To learn more about Next.js, take a look at the following resources:

- [Next.js Documentation](https://nextjs.org/docs) - learn about Next.js features and API.
- [Learn Next.js](https://nextjs.org/learn) - an interactive Next.js tutorial.

You can check out [the Next.js GitHub repository](https://github.com/vercel/next.js/) - your feedback and contributions
are welcome!
