// M6-001：扩展 Vben 用户信息，携带“必须修改口令”标记。
// 后端 /auth/user/info 返回 mustChangePassword，前端据此强制改密。
declare module '@vben/types' {
  interface UserInfo {
    mustChangePassword?: boolean;
    totpEnabled?: boolean;
  }
}

export {};
