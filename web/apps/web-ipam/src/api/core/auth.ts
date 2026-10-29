import { baseRequestClient, requestClient } from '#/api/request';

export namespace AuthApi {
  /** 登录接口参数 */
  export interface LoginParams {
    password?: string;
    username?: string;
  }

  /** 登录接口返回值 */
  export interface LoginResult {
    accessToken: string;
    refreshToken: string;
    /** M6-001：首次登录/管理员重置后必须修改口令 */
    mustChangePassword?: boolean;
  }

  /** 刷新令牌接口返回值（M5-032：baseRequestClient 为 raw 响应，data 体对齐 AuthRefreshResult） */
  export interface RefreshTokenResult {
    data: {
      accessToken: string;
      refreshToken: string;
    };
    status: number;
  }
}

/**
 * 登录
 */
export async function loginApi(data: AuthApi.LoginParams) {
  return requestClient.post<AuthApi.LoginResult>('/auth/login', data);
}

/**
 * 修改当前账号口令（M6-001）
 * 成功后返回新的令牌对；旧令牌因 token_version 递增立即失效。
 */
export async function changePasswordApi(data: {
  newPassword: string;
  oldPassword: string;
}) {
  return requestClient.post<AuthApi.LoginResult>('/auth/password', data);
}

/**
 * 刷新accessToken（M5-032）
 * 后端契约：请求体 refreshToken 优先；缺省时降级校验 Authorization Bearer 访问令牌。
 * 当前前端未持久化 refreshToken（accessStore 无该字段），走降级路径——
 * 显式携带现有访问令牌（即使已过期，签名有效且处于宽限窗口即可换发）。
 */
export async function refreshTokenApi(accessToken?: null | string) {
  return baseRequestClient.post<AuthApi.RefreshTokenResult>(
    '/auth/refresh',
    {},
    {
      headers: accessToken
        ? { Authorization: `Bearer ${accessToken}` }
        : undefined,
      withCredentials: true,
    },
  );
}

/**
 * 退出登录
 */
export async function logoutApi() {
  return baseRequestClient.post('/auth/logout', {
    withCredentials: true,
  });
}

/**
 * 获取用户权限码
 */
export async function getAccessCodesApi() {
  return requestClient.get<string[]>('/auth/codes');
}
