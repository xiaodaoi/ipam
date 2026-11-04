export interface OnboardingState {
  /** 管理员点过「完成向导」。 */
  completed?: boolean;
  /** 管理员点过「稍后再说/忽略」。 */
  dismissed?: boolean;
  updatedAt?: string;
}

export interface OnboardingCheck {
  done: boolean;
  /** 检测失败/无权限时的说明。 */
  error?: string;
  /** 当前值摘要，如「3 个子网」。 */
  hint?: string;
}
