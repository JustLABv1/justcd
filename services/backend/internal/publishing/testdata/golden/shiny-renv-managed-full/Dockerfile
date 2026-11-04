FROM registry.example.com/base/shiny:1
USER 0
WORKDIR /app
COPY source/ /app/
RUN R -e 'install.packages("renv", repos=Sys.getenv("R_REPOSITORY", "https://cloud.r-project.org")); renv::restore(prompt=FALSE)'
RUN chgrp -R 0 /app && chmod -R g=u /app
ENV HOME=/tmp
USER 1001
EXPOSE 3838
CMD ["R","-e","shiny::runApp('.', host='0.0.0.0', port=3838)"]
