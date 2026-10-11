import type { Vnode } from 'mithril'

/**
 * JSX typings for Mithril hyperscript.
 *
 * The SPA compiles TSX with `jsxFactory: "m"`, so expressions like
 * `<div class="x">{child}</div>` are handed straight to Mithril's hyperscript
 * and produce vnodes, not framework objects. Mithril ships no JSX namespace of
 * its own, so this declares the minimal one TypeScript needs.
 *
 * `ElementType` is what lets a tag be a component object (`<ServiceCard/>`)
 * as well as a string (`<div/>`): hyperscript accepts both and turns them into
 * the same kind of vnode. Attribute checking stays loose on purpose — unknown
 * attributes are simply forwarded to the DOM node at runtime.
 */
declare global {
  namespace JSX {
    type Element = Vnode
    type ElementType =
      | string
      | ((props: Record<string, unknown>) => Element)
      | { view: (...args: any[]) => any }

    interface ElementChildrenAttribute {
      children: unknown
    }

    interface IntrinsicAttributes {
      key?: string | number
    }

    interface IntrinsicElements {
      [elemName: string]: Record<string, unknown>
    }
  }
}

export {}