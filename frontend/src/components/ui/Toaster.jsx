import { Toaster as SonnerToaster } from "sonner";

export function Toaster() {
  return (
    <SonnerToaster
      position="top-right"
      richColors
      closeButton
      visibleToasts={3}
      containerClassName="pointer-events-none fixed top-2 right-2 z-40 flex flex-col items-end gap-2"
      toastOptions={{
        className:
          "pointer-events-auto w-full max-w-[420px] rounded-md border bg-background p-4 text-sm text-foreground shadow-md",
        descriptionClassName: "text-muted-foreground",
        actionButtonClassName:
          "bg-primary text-primary-foreground hover:bg-primary/90",
        cancelButtonClassName:
          "bg-muted text-muted-foreground hover:bg-muted/90",
      }}
    />
  );
}

export default Toaster;
