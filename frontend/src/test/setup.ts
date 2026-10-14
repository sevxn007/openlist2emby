import "@testing-library/jest-dom/vitest"

Object.defineProperty(URL, "createObjectURL", {
  writable: true,
  value: () => "blob:test-image",
})

Object.defineProperty(URL, "revokeObjectURL", {
  writable: true,
  value: () => undefined,
})

Object.defineProperty(HTMLElement.prototype, "hasPointerCapture", {
  writable: true,
  value: () => false,
})

Object.defineProperty(HTMLElement.prototype, "setPointerCapture", {
  writable: true,
  value: () => undefined,
})

Object.defineProperty(HTMLElement.prototype, "releasePointerCapture", {
  writable: true,
  value: () => undefined,
})

Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
  writable: true,
  value: () => undefined,
})

Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
  writable: true,
  value: () => ({
    clearRect: () => undefined,
    fillRect: () => undefined,
    getImageData: () => ({ data: [0, 0, 0, 0] }),
    putImageData: () => undefined,
    createImageData: () => [],
    setTransform: () => undefined,
    drawImage: () => undefined,
    save: () => undefined,
    fillText: () => undefined,
    restore: () => undefined,
    beginPath: () => undefined,
    moveTo: () => undefined,
    lineTo: () => undefined,
    closePath: () => undefined,
    stroke: () => undefined,
    translate: () => undefined,
    scale: () => undefined,
    rotate: () => undefined,
    arc: () => undefined,
    fill: () => undefined,
    measureText: () => ({ width: 0 }),
    transform: () => undefined,
    rect: () => undefined,
    clip: () => undefined,
  }),
})

class ResizeObserverMock {
  observe() {
    return undefined
  }

  unobserve() {
    return undefined
  }

  disconnect() {
    return undefined
  }
}

class IntersectionObserverMock {
  readonly root = null
  readonly rootMargin = ""
  readonly thresholds: number[] = []

  observe() {
    return undefined
  }

  unobserve() {
    return undefined
  }

  disconnect() {
    return undefined
  }

  takeRecords() {
    return []
  }
}

Object.defineProperty(window, "ResizeObserver", {
  writable: true,
  value: ResizeObserverMock,
})

Object.defineProperty(globalThis, "ResizeObserver", {
  writable: true,
  value: ResizeObserverMock,
})

Object.defineProperty(window, "IntersectionObserver", {
  writable: true,
  value: IntersectionObserverMock,
})

Object.defineProperty(globalThis, "IntersectionObserver", {
  writable: true,
  value: IntersectionObserverMock,
})

if (typeof Range !== "undefined") {
  Object.defineProperty(Range.prototype, "getBoundingClientRect", {
    writable: true,
    value: () => ({
      bottom: 0,
      height: 0,
      left: 0,
      right: 0,
      top: 0,
      width: 0,
      x: 0,
      y: 0,
      toJSON: () => undefined,
    }),
  })

  Object.defineProperty(Range.prototype, "getClientRects", {
    writable: true,
    value: () => [],
  })
}

Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  }),
})
