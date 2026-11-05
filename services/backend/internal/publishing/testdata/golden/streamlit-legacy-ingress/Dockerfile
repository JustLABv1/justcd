FROM registry.example.com/base/shiny:1
USER 0
WORKDIR /app
COPY source/ /app/
RUN python -m pip install --no-cache-dir -r requirements.txt
RUN chgrp -R 0 /app && chmod -R g=u /app
ENV HOME=/tmp
USER 1001
EXPOSE 8501
CMD ["python","-m","streamlit","run","app.py","--server.address=0.0.0.0","--server.port=8501"]
