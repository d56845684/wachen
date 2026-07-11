/**
 * Session store（zustand）— 登入狀態、目前角色、token。
 * token 由 POST /api/v1/login 取得，persist 到 AsyncStorage。
 */
import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { ROLES, type Role, type RoleId } from './roles';

interface SessionState {
  token: string | null;
  roleId: RoleId;
  role: Role;
  signIn: (token: string, roleId: RoleId) => void;
  switchRole: (roleId: RoleId) => void; // PoC demo 用；正式版下架
  signOut: () => void;
}

export const useSession = create<SessionState>()(
  persist(
    (set) => ({
      token: null,
      roleId: 'hq',
      role: ROLES.hq,
      signIn: (token, roleId) => set({ token, roleId, role: ROLES[roleId] }),
      switchRole: (roleId) => set({ roleId, role: ROLES[roleId] }),
      signOut: () => set({ token: null }),
    }),
    { name: 'wacity-session', storage: createJSONStorage(() => AsyncStorage) },
  ),
);
