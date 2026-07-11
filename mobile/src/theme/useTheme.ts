import { useColorScheme } from 'react-native';
import { dark, light, type Palette } from './tokens';

export function useTheme(): Palette {
  return useColorScheme() === 'dark' ? dark : light;
}
