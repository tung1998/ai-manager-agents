export default defineAppConfig({
  ui: {
    colors: {
      primary: 'indigo',
      neutral: 'zinc'
    },
    // a long title wraps before the close button instead of running under it
    modal: { slots: { title: 'pe-8', description: 'pe-8' } },
    slideover: { slots: { title: 'pe-8', description: 'pe-8' } },
    // dropdown content width defaults to the trigger button width (w-(--reka-*-trigger-width)),
    // which shrinks to the icon on mobile when the trigger label is hidden — min-w-fit wins over width
    select: { slots: { content: 'min-w-fit max-w-[calc(100vw-1rem)]' } },
    selectMenu: { slots: { content: 'min-w-fit max-w-[calc(100vw-1rem)]' } },
    inputMenu: { slots: { content: 'min-w-fit max-w-[calc(100vw-1rem)]' } }
  }
})
