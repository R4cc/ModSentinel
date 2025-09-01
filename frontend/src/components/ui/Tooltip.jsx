import { cloneElement, useId, useState, useRef } from "react";
import { createPortal } from "react-dom";
import { motion, AnimatePresence, useReducedMotion } from "framer-motion";

export function Tooltip({ children, text }) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [coords, setCoords] = useState({ top: 0, left: 0, placement: "bottom" });
  const reduce = useReducedMotion();
  const ref = useRef(null);

  const trigger = cloneElement(children, {
    onMouseEnter: (e) => {
      children.props.onMouseEnter?.(e);
      const rect = e.currentTarget.getBoundingClientRect();
      const margin = 6;
      const preferredTop = rect.bottom + margin;
      let placement = "bottom";
      let top = preferredTop;
      // If close to viewport bottom, place above
      if (preferredTop > (window.innerHeight || 0) - 40) {
        top = rect.top - margin;
        placement = "top";
      }
      setCoords({ left: rect.left + rect.width / 2, top, placement });
      setOpen(true);
    },
    onMouseLeave: (e) => {
      children.props.onMouseLeave?.(e);
      setOpen(false);
    },
    onFocus: (e) => {
      children.props.onFocus?.(e);
      const rect = e.currentTarget.getBoundingClientRect();
      const margin = 6;
      const preferredTop = rect.bottom + margin;
      let placement = "bottom";
      let top = preferredTop;
      if (preferredTop > (window.innerHeight || 0) - 40) {
        top = rect.top - margin;
        placement = "top";
      }
      setCoords({ left: rect.left + rect.width / 2, top, placement });
      setOpen(true);
    },
    onBlur: (e) => {
      children.props.onBlur?.(e);
      setOpen(false);
    },
    "aria-describedby": id,
  });

  return (
    <div ref={ref} className="inline-block align-middle">
      {trigger}
      {createPortal(
        <AnimatePresence>
          {open && (
            <motion.div
              id={id}
              role="tooltip"
              initial={{ opacity: 0, y: reduce ? 0 : 4 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: reduce ? 0 : 4 }}
              transition={{ duration: reduce ? 0 : 0.15 }}
              style={{ position: "fixed", left: coords.left, top: coords.top, transform: "translateX(-50%)" }}
              className="pointer-events-none z-[9999] whitespace-nowrap rounded bg-gray-900 px-2 py-1 text-xs text-white shadow-md"
            >
              {text}
            </motion.div>
          )}
        </AnimatePresence>,
        document.body
      )}
    </div>
  );
}

export default Tooltip;
