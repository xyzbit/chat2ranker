import { useLayoutEffect, useRef } from "react";

/**
 * Native modal focus containment, Escape dismissal, and focus restoration.
 * @param props Accessible label, dismissal callback, optional backdrop class, and content.
 * @returns A modal dialog in the browser top layer.
 */
export function Modal({ label, onClose, className = "", children }) {
  const ref = useRef(null);
  useLayoutEffect(() => {
    const dialog = ref.current;
    dialog.showModal();
    return () => dialog.close();
  }, []);
  return <dialog ref={ref} aria-label={label} className={`modal-backdrop ${className}`}
    onCancel={(event) => { event.preventDefault(); onClose(); }}
    onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
    {children}
  </dialog>;
}
