// 图形与逐元素动效移植自 Lucide Animated；许可与版本见 ../README.md。
// https://github.com/pqoqubbw/icons/blob/072c38b1b04ea738d90a084485ccaad4b890ddca/icons/languages.tsx
import { h } from 'vue'
import { motion } from 'motion-v'
import type { IconDefinition, IconVariants as Variants } from '../types'

// custom 是笔画的出场序号，序号大的笔画晚 0.1s 开始重画。
const PATH_VARIANTS: Variants = {
  normal: { opacity: 1, pathLength: 1, pathOffset: 0 },
  animate: (custom: number) => ({
    opacity: [0, 1],
    pathLength: [0, 1],
    pathOffset: [1, 0],
    transition: {
      opacity: { duration: 0.01, delay: custom * 0.1 },
      pathLength: {
        type: 'spring',
        duration: 0.5,
        bounce: 0,
        delay: custom * 0.1
      }
    }
  })
}

const icon: IconDefinition = {
  name: 'languages',
  normal: 'normal',
  animate: ['animate'],
  render: (controls) =>
    h('g', {}, [
      h(
        motion.path,
        {
          animate: controls,
          custom: 3,
          d: 'm5 8 6 6',
          initial: 'normal',
          variants: PATH_VARIANTS
        },
        () => []
      ),
      h(
        motion.path,
        {
          animate: controls,
          custom: 2,
          d: 'm4 14 6-6 3-3',
          initial: 'normal',
          variants: PATH_VARIANTS
        },
        () => []
      ),
      h(
        motion.path,
        {
          animate: controls,
          custom: 1,
          d: 'M2 5h12',
          initial: 'normal',
          variants: PATH_VARIANTS
        },
        () => []
      ),
      h(
        motion.path,
        {
          animate: controls,
          custom: 0,
          d: 'M7 2h1',
          initial: 'normal',
          variants: PATH_VARIANTS
        },
        () => []
      ),
      h(
        motion.path,
        {
          animate: controls,
          custom: 3,
          d: 'm22 22-5-10-5 10',
          initial: 'normal',
          variants: PATH_VARIANTS
        },
        () => []
      ),
      h(
        motion.path,
        {
          animate: controls,
          custom: 3,
          d: 'M14 18h6',
          initial: 'normal',
          variants: PATH_VARIANTS
        },
        () => []
      )
    ])
}

export default icon
