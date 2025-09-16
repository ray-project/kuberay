import { NextResponse } from "next/server";
import { defaultConfig } from "@/utils/constants";

export async function GET() {
  const config = {
    apiUrl:
      process.env.NEXT_PUBLIC_API_URL ||
      process.env.API_URL ||
      defaultConfig.url,
  };

  return NextResponse.json(config);
}
