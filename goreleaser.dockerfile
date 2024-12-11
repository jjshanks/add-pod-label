FROM scratch
COPY add-pod-label /add-pod-label
ENTRYPOINT ["/add-pod-label"]