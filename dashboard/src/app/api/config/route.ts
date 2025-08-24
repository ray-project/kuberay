import { NextResponse } from "next/server";

export async function GET() {
  const config = {
    apiUrl:
      process.env.NEXT_PUBLIC_API_URL ||
      process.env.API_URL ||
      "http://localhost:31888/apis/v1",
  };

  return NextResponse.json(config);
}
