import { notFound } from "next/navigation"
import { PlanReviewFixture } from "@/components/plan-review-fixture"

export const dynamic = "force-dynamic"

export default function FixturePage() {
  if (process.env.JUSTCD_PLAN_REVIEW_FIXTURE !== "1") notFound()
  return <PlanReviewFixture />
}
