import { Redirect } from 'expo-router';
import { useSession } from '@/auth/session';

export default function Index() {
  const token = useSession((s) => s.token);
  return <Redirect href={token ? '/(tabs)' : '/login'} />;
}
