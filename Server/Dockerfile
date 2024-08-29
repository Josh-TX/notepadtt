# This dockerfile assumes that /Server/wwwroot/ already contains the built UI
# Stage 1 - Publish dotnet 
FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:8.0 AS dotnet-build

ARG TARGETARCH
WORKDIR /source
COPY Notepadtt.csproj ./
RUN dotnet restore
COPY . .
RUN dotnet publish -a $TARGETARCH -o /app

# final stage/image
FROM mcr.microsoft.com/dotnet/aspnet:8.0

WORKDIR /app
COPY --from=dotnet-build /app .
RUN mkdir /data && chown $APP_UID /data
VOLUME /app/data
USER $APP_UID

ENTRYPOINT ["./Notepadtt"]
