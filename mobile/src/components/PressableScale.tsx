/**
 * 全 app 唯一的可按元件基底。
 * emil-design-eng：按壓必須有回饋 — pointer-down 當下 scale 0.97（不是放開才動），
 * 140ms 強 ease-out；命中範圍外擴 10px（apple-design §10 hysteresis）。
 */
import React from 'react';
import { Pressable, type PressableProps, type ViewStyle } from 'react-native';
import Animated, { useAnimatedStyle, useSharedValue, withTiming, ReduceMotion } from 'react-native-reanimated';
import { duration, easeOut } from '@/theme/motion';

const AnimatedPressable = Animated.createAnimatedComponent(Pressable);

interface Props extends PressableProps {
  scaleTo?: number;
  style?: ViewStyle | ViewStyle[];
}

export function PressableScale({ scaleTo = 0.97, style, onPressIn, onPressOut, ...rest }: Props) {
  const scale = useSharedValue(1);
  const animated = useAnimatedStyle(() => ({ transform: [{ scale: scale.value }] }));

  return (
    <AnimatedPressable
      hitSlop={10}
      onPressIn={(e) => {
        // 回饋在 press-in 當下，絕不等 press-up
        scale.value = withTiming(scaleTo, { duration: duration.press, easing: easeOut, reduceMotion: ReduceMotion.System });
        onPressIn?.(e);
      }}
      onPressOut={(e) => {
        scale.value = withTiming(1, { duration: duration.press, easing: easeOut, reduceMotion: ReduceMotion.System });
        onPressOut?.(e);
      }}
      style={[animated, style]}
      {...rest}
    />
  );
}
