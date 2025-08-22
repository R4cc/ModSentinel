import { cloneElement, useId, useState } from "react";
import { motion, AnimatePresence, useReducedMotion } from "framer-motion";

export function Tooltip({ children, text }) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const reduce = useReducedMotion();

  const trigger = cloneElement(children, {
    onMouseEnter: (e) => {
      children.props.onMouseEnter?.(e);
      setOpen(true);
    },
    onMouseLeave: (e) => {
      children.props.onMouseLeave?.(e);
      setOpen(false);
    },
    onFocus: (e) => {
      children.props.onFocus?.(e);
      setOpen(true);
    },
    onBlur: (e) => {
      children.props.onBlur?.(e);
      setOpen(false);
    },
    "aria-describedby": id,
  });

  return (
    <div className="relative inline-block">
      {trigger}
      <AnimatePresence>
        {open && (
          <motion.div
            id={id}
            role="tooltip"
            initial={{ opacity: 0, y: reduce ? 0 : 4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: reduce ? 0 : 4 }}
            transition={{ duration: reduce ? 0 : 0.15 }}
            className="pointer-events-none absolute left-1/2 top-full z-20 mt-1 -translate-x-1/2 whitespace-nowrap rounded bg-gray-900 px-2 py-1 text-xs text-white shadow-md"
          >
            {text}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

export default Tooltip;
