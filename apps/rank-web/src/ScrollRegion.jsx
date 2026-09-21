import { useLayoutEffect, useRef } from "react";

/**
 * Scrollable content with edge fades only where more content exists.
 * @param props Element attributes, an optional scrollRef, and optional tail-following for conversation updates.
 * @returns A keyboard-scrollable region with no visible scrollbar.
 */
export function ScrollRegion({ as: Element = "div", className = "", followTail = false, scrollRef, children, ...props }) {
  const localRef = useRef(null);
  const ref = scrollRef || localRef;
  useLayoutEffect(() => {
    const node = ref.current;
    let atEnd = true;
    let frame = 0;
    const update = () => {
      const remaining = node.scrollHeight - node.clientHeight - node.scrollTop;
      atEnd = remaining < 36;
      node.dataset.scrollTop = String(node.scrollTop > 2);
      node.dataset.scrollBottom = String(remaining > 2);
    };
    const resize = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        if (followTail && atEnd) node.scrollTop = node.scrollHeight;
        update();
      });
    };
    const observer = new ResizeObserver(resize);
    const observeChildren = () => {
      observer.disconnect();
      observer.observe(node);
      for (const child of node.children) observer.observe(child);
      resize();
    };
    const mutations = new MutationObserver(observeChildren);
    mutations.observe(node, { childList: true, subtree: true, characterData: true });
    node.addEventListener("scroll", update, { passive: true });
    if (followTail) node.scrollTop = node.scrollHeight;
    update();
    observeChildren();
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      mutations.disconnect();
      node.removeEventListener("scroll", update);
    };
  }, [followTail, ref]);

  return <Element ref={ref} tabIndex={0} className={`scroll-region ${className}`} {...props}>{children}</Element>;
}
