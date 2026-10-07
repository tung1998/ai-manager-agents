// Burn review profiles (ADR-113): saved review setups of a project, each
// stage with its own reviewer (GET /api/projects/{id}/burn/review-profiles).
export type ReviewStage = 'issue' | 'plan' | 'result'
export const reviewStageKeys: ReviewStage[] = ['issue', 'plan', 'result']

export interface BurnReviewStage {
  agent_id: string // '' = the Burn's main agent
  workflow: string // '' = the agent reviews itself
}

export interface BurnReviewProfile {
  id: string
  name: string
  stages: Partial<Record<ReviewStage, BurnReviewStage>> // absent = not reviewed
  in_use: boolean
}
