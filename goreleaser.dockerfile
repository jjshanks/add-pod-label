FROM scratch
COPY add-pod-label /pod-label-webhook
ENTRYPOINT ["/pod-label-webhook"]