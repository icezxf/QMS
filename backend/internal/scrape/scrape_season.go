package scrape

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"qmediasync/internal/db"
	"qmediasync/internal/douban"
	"qmediasync/internal/helpers"
	"qmediasync/internal/models"
	"qmediasync/internal/tmdb"
	"qmediasync/internal/v115open"
)

func (t tvShowScrapeImpl) GetSeasonUploadFiles(seasonMediaFile *models.ScrapeMediaFile) []uploadFile {
	destPath := seasonMediaFile.GetDestFullSeasonPath()
	destPathId := seasonMediaFile.NewSeasonPathId
	if destPathId == "" {
		destPathId = seasonMediaFile.NewPathId
	}
	sourcePath := seasonMediaFile.GetTmpFullSeasonPath()
	tvshowSourcePath := seasonMediaFile.GetTmpFullTvshowPath()
	tvshowPath := seasonMediaFile.GetDestFullTvshowPath()
	tvshowPathId := seasonMediaFile.NewPathId
	fileList := make([]uploadFile, 0)
	nfoName := seasonMediaFile.GetSeasonNfoName()
	nfoFullSourceName := filepath.Join(sourcePath, nfoName)
	file := uploadFile{
		ID:         fmt.Sprintf("%d", seasonMediaFile.ID),
		DestPathId: destPathId,
		DestPath:   destPath,
		FileName:   nfoName,
		SourcePath: nfoFullSourceName,
	}
	fileList = append(fileList, file)
	posterName := fmt.Sprintf("season%02d-poster.jpg", seasonMediaFile.SeasonNumber)
	file = uploadFile{
		ID:         fmt.Sprintf("%d", seasonMediaFile.ID),
		DestPathId: tvshowPathId,
		DestPath:   tvshowPath,
		FileName:   posterName,
		SourcePath: filepath.Join(tvshowSourcePath, posterName),
	}
	fileList = append(fileList, file)
	return fileList
}

func (t *tvShowScrapeImpl) UploadSeasonScrapeFile(seasonMediaFile *models.ScrapeMediaFile) error {
	helpers.AppLogger.Infof("开始处理电视剧 %s 季 %d 的元数据", seasonMediaFile.Name, seasonMediaFile.SeasonNumber)
	files := t.GetSeasonUploadFiles(seasonMediaFile)
	ok, err := t.MoveLocalTempFileToDest(seasonMediaFile, files)
	if err == nil {
		return nil
	}
	if !ok {
		return err
	}
	for _, file := range files {
		err := models.AddUploadTaskFromMediaFile(seasonMediaFile, t.scrapePath, file.FileName, file.SourcePath, filepath.Join(file.DestPath, file.FileName), file.DestPathId, true)
		if err != nil {
			helpers.AppLogger.Errorf("添加上传任务 %s 失败, 失败原因: %v", file.FileName, err)
		}
	}
	helpers.AppLogger.Infof("完成电视剧 %s 季 %d 的元数据处理", seasonMediaFile.Name, seasonMediaFile.SeasonNumber)
	return nil
}

func (t *tvShowScrapeImpl) ScrapeFailedAllEpisodeBySeason(seasonMediaFile *models.ScrapeMediaFile, failedReason string) error {
	err := db.Db.Table("scrape_media_files").Where("tvshow_path = ? AND batch_no = ? AND season_number = ?", seasonMediaFile.TvshowPathId, seasonMediaFile.BatchNo, seasonMediaFile.SeasonNumber).Updates(map[string]interface{}{
		"status":        models.ScrapeMediaStatusScrapeFailed,
		"failed_reason": failedReason,
	}).Error
	if err != nil {
		helpers.AppLogger.Errorf("批量更新电视剧 %s 季 %d 的所有集为刮削失败状态失败, 失败原因: %v", seasonMediaFile.Name, seasonMediaFile.SeasonNumber, err)
		return err
	}
	seasonMediaFile.Failed(failedReason)
	helpers.AppLogger.Infof("批量更新电视剧 %s 季 %d 的所有集为刮削失败状态成功", seasonMediaFile.Name, seasonMediaFile.SeasonNumber)
	return nil
}

func (t *tvShowScrapeImpl) RenamedFailedAllEdpisodeBySeason(seasonMediaFile *models.ScrapeMediaFile, failedReason string) {
	err := db.Db.Table("scrape_media_files").Where("tvshow_path = ? AND batch_no = ? AND season_number = ?", seasonMediaFile.TvshowPathId, seasonMediaFile.BatchNo, seasonMediaFile.SeasonNumber).Updates(map[string]interface{}{
		"status":        models.ScrapeMediaStatusScrapeFailed,
		"failed_reason": failedReason,
	}).Error
	if err != nil {
		helpers.AppLogger.Errorf("批量更新电视剧 %s 季 %d 的所有集为刮削失败状态失败, 失败原因: %v", seasonMediaFile.Name, seasonMediaFile.SeasonNumber, err)
		return
	}
	helpers.AppLogger.Infof("批量更新电视剧 %s 季 %d 的所有集为刮削失败状态成功", seasonMediaFile.Name, seasonMediaFile.SeasonNumber)
}

func (t *tvShowScrapeImpl) UpdateSeasonDataToAllEpisodeBySeason(seasonMediaFile *models.ScrapeMediaFile) error {
	err := db.Db.Table("scrape_media_files").Where("media_id = ? AND batch_no = ? AND season_number = ?", seasonMediaFile.MediaId, seasonMediaFile.BatchNo, seasonMediaFile.SeasonNumber).Updates(map[string]interface{}{
		"new_season_path_id":   seasonMediaFile.NewSeasonPathId,
		"new_season_path_name": seasonMediaFile.NewSeasonPathName,
		"media_season_id":      seasonMediaFile.MediaSeasonId,
	}).Error
	if err != nil {
		helpers.AppLogger.Errorf("批量更新电视剧 %s 季 %d 的所有集的新目录ID失败, 失败原因: %v", seasonMediaFile.Name, seasonMediaFile.SeasonNumber, err)
		return err
	}
	helpers.AppLogger.Infof("批量更新电视剧 %s 季 %d 的所有集的新目录ID成功", seasonMediaFile.Name, seasonMediaFile.SeasonNumber)
	return nil
}

func (t *tvShowScrapeImpl) MakeSeasonPath(seasonMediaFile *models.ScrapeMediaFile) error {
	destFullPath := seasonMediaFile.GetDestFullSeasonPath()
	helpers.AppLogger.Infof("电视剧 %s 季 %d 文件夹，目标路径：%s", seasonMediaFile.Name, seasonMediaFile.SeasonNumber, destFullPath)
	if seasonMediaFile.ScrapeType == models.ScrapeTypeOnly {
		seasonMediaFile.NewSeasonPathId = seasonMediaFile.PathId
		return nil
	}
	newPathId, err := t.renameImpl.CheckAndMkDir(destFullPath, seasonMediaFile.DestPath, seasonMediaFile.DestPathId)
	if err != nil {
		helpers.AppLogger.Errorf("创建目录 %s 失败, 失败原因: %v", destFullPath, err)
		return err
	}
	seasonMediaFile.NewSeasonPathId = newPathId
	seasonMediaFile.MediaSeason.Path = destFullPath
	seasonMediaFile.MediaSeason.PathId = newPathId
	seasonMediaFile.MediaSeason.Save()
	helpers.AppLogger.Infof("电视剧 %s 季 %d的目标目录 %s 成功", seasonMediaFile.Name, seasonMediaFile.SeasonNumber, destFullPath)
	return nil
}

// 处理季的刮削
func (t *tvShowScrapeImpl) ProcessSeason(seasonMediaFile *models.ScrapeMediaFile) error {
	seasonMediaFile.ScrapeRootPath = filepath.Join(helpers.ConfigDir, "tmp", "刮削临时文件", fmt.Sprintf("%d", seasonMediaFile.ScrapePathId), "电视剧")
	if err := os.MkdirAll(seasonMediaFile.ScrapeRootPath, 0777); err != nil {
		helpers.AppLogger.Errorf("创建临时目录失败: %v", err)
		return err
	}
	if seasonMediaFile.MediaSeason == nil || (seasonMediaFile.MediaSeason != nil && seasonMediaFile.MediaSeason.Status == models.MediaStatusUnScraped) {
		serr := t.ScrapeSeasonMedia(seasonMediaFile)
		if serr != nil {
			t.ScrapeFailedAllEpisodeBySeason(seasonMediaFile, serr.Error())
			return serr
		}
	}
	if seasonMediaFile.MediaSeason != nil && seasonMediaFile.MediaSeason.Status == models.MediaStatusScraped {
		if t.scrapePath.ScrapeType != models.ScrapeTypeOnlyRename {
			if uerr := t.UploadSeasonScrapeFile(seasonMediaFile); uerr != nil {
				t.RenamedFailedAllEdpisodeBySeason(seasonMediaFile, uerr.Error())
				return uerr
			}
			seasonMediaFile.MediaSeason.Status = models.MediaStatusRenamed
			seasonMediaFile.MediaSeason.Save()
		}
	}
	return nil
}

func (t *tvShowScrapeImpl) ScrapeSeasonMedia(mediaFile *models.ScrapeMediaFile) error {
	seasonNumber := mediaFile.SeasonNumber
	if mediaFile.MediaSeason != nil && mediaFile.MediaSeason.Status != models.MediaStatusUnScraped {
		helpers.AppLogger.Infof("电视剧 %s 季 %d 已刮削完毕，跳过刮削", mediaFile.Name, seasonNumber)
		return nil
	}
	seasonDetail, err := t.tmdbClient.GetTvSeasonDetail(mediaFile.TmdbId, mediaFile.SeasonNumber, models.GlobalScrapeSettings.GetTmdbLanguage())
	if err != nil {
		helpers.AppLogger.Errorf("查询tmdb电视剧季详情失败,下次重试, 失败原因: %v", err)
		return err
	}
	if mediaFile.MediaSeasonId == 0 {
		mediaFile.MediaSeason = &models.MediaSeason{
			MediaId:      mediaFile.MediaId,
			SeasonNumber: mediaFile.SeasonNumber,
		}
	}
	t.MakeMediaSeasonFromTMDB(mediaFile, seasonDetail)
	mediaFile.NewSeasonPathName = mediaFile.GetDestSeasonPath()

	// ===== 豆瓣季评分补全（必须在 GenerateSeasonNfo 之前）=====
	t.enrichSeasonWithDoubanRating(mediaFile)
	// ===== 结束 =====

	if mediaFile.ScrapeType != models.ScrapeTypeOnlyRename {
		localTempSeasonPath := mediaFile.GetTmpFullSeasonPath()
		if mkdirErr := os.MkdirAll(localTempSeasonPath, 0777); mkdirErr != nil {
			helpers.AppLogger.Errorf("创建临时目录失败, 失败原因: %v", mkdirErr)
			t.ScrapeFailedAllEpisodeBySeason(mediaFile, mkdirErr.Error())
			return mkdirErr
		} else {
			helpers.AppLogger.Infof("季临时刮削文件存储路径创建成功，电视剧 %s 的第 %d 季: %s", mediaFile.Name, mediaFile.SeasonNumber, localTempSeasonPath)
		}
		t.GenerateSeasonNfo(mediaFile)
		seasonImageList := make(map[string]string)
		seasonPosterFile := fmt.Sprintf("season%02d-poster.jpg", mediaFile.SeasonNumber)
		seasonImageList[seasonPosterFile] = mediaFile.MediaSeason.PosterPath
		localTempTvshowPath := mediaFile.GetTmpFullTvshowPath()
		t.DownloadImages(localTempTvshowPath, v115open.DEFAULTUA, seasonImageList)
		helpers.AppLogger.Infof("电视剧 %s 季 %d 的封面文件已下载，路径: %s", mediaFile.Name, mediaFile.SeasonNumber, filepath.Join(localTempTvshowPath, seasonPosterFile))
	}
	if err := t.MakeSeasonPath(mediaFile); err != nil {
		return err
	}
	if err := t.UpdateSeasonDataToAllEpisodeBySeason(mediaFile); err != nil {
		return err
	}
	return nil
}

// enrichSeasonWithDoubanRating 用豆瓣评分覆盖该季的评分
func (t *tvShowScrapeImpl) enrichSeasonWithDoubanRating(mediaFile *models.ScrapeMediaFile) {
	if mediaFile.MediaSeason == nil || mediaFile.Media == nil {
		return
	}

	doubanClient := douban.NewClient("")

	// 优先：季独立 IMDb ID（欧美剧如果 TMDB 有返回，最精准）
	externalIds, eerr := t.tmdbClient.GetTvSeasonExternalIds(mediaFile.TmdbId, mediaFile.SeasonNumber)
	if eerr == nil && externalIds != nil && externalIds.ImdbId != "" {
		rating, err := doubanClient.GetTVRatingByImdb(externalIds.ImdbId)
		if err == nil && rating > 0 {
			oldRating := mediaFile.MediaSeason.VoteAverage
			mediaFile.MediaSeason.VoteAverage = rating
			mediaFile.MediaSeason.Save()
			helpers.AppLogger.Infof("[豆瓣] 季评分已更新(IMDb): %s 第 %d 季 (%.1f -> %.1f)",
				mediaFile.Name, mediaFile.SeasonNumber, oldRating, rating)
			return
		}
	}

	// 回退：用「剧名+季号」搜索豆瓣季条目
	rating, err := doubanClient.GetSeasonRatingByTitle(
		mediaFile.Media.Name,
		mediaFile.Media.OriginalName,
		mediaFile.SeasonNumber,
	)
	if err != nil {
		helpers.AppLogger.Warnf("[豆瓣] 搜索季评分失败: %v, 剧集: %s 季 %d", err, mediaFile.Name, mediaFile.SeasonNumber)
		return
	}

	if rating > 0 {
		oldRating := mediaFile.MediaSeason.VoteAverage
		mediaFile.MediaSeason.VoteAverage = rating
		mediaFile.MediaSeason.Save()
		helpers.AppLogger.Infof("[豆瓣] 季评分已更新(搜索): %s 第 %d 季 (%.1f -> %.1f)",
			mediaFile.Name, mediaFile.SeasonNumber, oldRating, rating)
	} else {
		helpers.AppLogger.Infof("[豆瓣] 未找到季评分: %s 第 %d 季", mediaFile.Name, mediaFile.SeasonNumber)
	}
}

func (sm *tvShowScrapeImpl) GenerateSeasonNfo(mediaFile *models.ScrapeMediaFile) error {
	ratingStr := ""
	if mediaFile.MediaSeason.VoteAverage > 0 {
		ratingStr = fmt.Sprintf("%.1f", mediaFile.MediaSeason.VoteAverage)
	}
	season := &helpers.TVShowSeason{
		Title:         mediaFile.MediaSeason.SeasonName,
		OriginalTitle: mediaFile.MediaSeason.SeasonName,
		Premiered:     mediaFile.MediaSeason.ReleaseDate,
		Releasedate:   mediaFile.MediaSeason.ReleaseDate,
		Year:          mediaFile.MediaSeason.Year,
		SeasonNumber:  mediaFile.MediaSeason.SeasonNumber,
		DateAdded:     time.Now().Format("2006-01-02"),
		Rating:        ratingStr,
		UserRating:    ratingStr,
	}
	seasonPath := mediaFile.GetTmpFullSeasonPath()
	seasonFileName := mediaFile.GetSeasonNfoName()
	seasonNfoFile := filepath.Join(seasonPath, seasonFileName)
	err := helpers.WriteSeasonNfo(season, seasonNfoFile)
	if err != nil {
		helpers.AppLogger.Errorf("生成电视剧 %s 季 %d 的nfo文件失败: %v", mediaFile.Name, mediaFile.MediaSeason.SeasonNumber, err)
		return err
	}
	helpers.AppLogger.Infof("生成电视剧 %s 季 %d 的nfo文件成功: %s", mediaFile.Name, mediaFile.MediaSeason.SeasonNumber, seasonNfoFile)
	return nil
}

func (t *tvShowScrapeImpl) MakeMediaSeasonFromTMDB(mediaFile *models.ScrapeMediaFile, seasonDetail *tmdb.SeasonDetail) {
	if mediaFile.MediaSeasonId != 0 {
		mediaSeason := models.GetSeasonByMediaIdAndSeasonNumber(mediaFile.MediaId, mediaFile.SeasonNumber)
		if mediaSeason != nil {
			mediaFile.MediaSeason = mediaSeason
		}
	}
	if mediaFile.MediaSeason == nil {
		mediaFile.MediaSeason = &models.MediaSeason{
			MediaId:      mediaFile.MediaId,
			SeasonNumber: mediaFile.SeasonNumber,
			ScrapePathId: mediaFile.ScrapePathId,
		}
	}
	mediaFile.MediaSeason.FillInfoByTmdbInfo(seasonDetail)
	mediaFile.MediaSeasonId = mediaFile.MediaSeason.ID
	mediaFile.Save()
}

func (t *tvShowScrapeImpl) RollbackTvShowSeason(mediaFile *models.ScrapeMediaFile) error {
	if mediaFile.ScrapeType == models.ScrapeTypeOnly {
		uploadFiles := t.GetSeasonUploadFiles(mediaFile)
		files := make([]models.WillDeleteFile, 0)
		for _, uf := range uploadFiles {
			files = append(files, models.WillDeleteFile{FullFilePath: filepath.Join(uf.DestPath, uf.FileName)})
		}
		err := t.renameImpl.CheckAndDeleteFiles(mediaFile, files)
		if err != nil {
			helpers.AppLogger.Errorf("删除已上传的元数据文失败: %v", err)
			return err
		}
		helpers.AppLogger.Infof("删除已上传的元数据文件成功: %v", files)
	}
	if mediaFile.ScrapeType == models.ScrapeTypeScrapeAndRename || mediaFile.ScrapeType == models.ScrapeTypeOnlyRename && mediaFile.Path != "" {
		parentPath := filepath.Dir(mediaFile.Path)
		var newPath string
		var pathId string
		var existsPathId string = ""
		newPath = filepath.Join(parentPath, mediaFile.NewSeasonPathName)
		if mediaFile.RenameType != models.RenameTypeMove {
			var eerr error
			existsPathId, eerr = t.renameImpl.ExistsAndRename(mediaFile.PathId, mediaFile.NewSeasonPathName)
			if eerr != nil {
				helpers.AppLogger.Errorf("重命名旧文件夹 %s 失败: %v", mediaFile.Path, eerr)
				return eerr
			}
		}
		if existsPathId == "" {
			var err error
			pathId, err = t.renameImpl.CheckAndMkDir(newPath, mediaFile.TvshowPath, mediaFile.TvshowPathId)
			if err != nil {
				helpers.AppLogger.Errorf("创建父文件夹 %s 失败: %v", newPath, err)
				return err
			}
		} else {
			pathId = existsPathId
		}
		mediaFile.Path = newPath
		mediaFile.PathId = pathId
		err := t.UpdateSeasonPathAndIdToAllEpisode(mediaFile)
		if err != nil {
			helpers.AppLogger.Errorf("更新电视剧 %s 的所有集的路径失败: %v", mediaFile.Name, err)
			return err
		}
	}
	mediaFile.MediaSeason.Status = models.MediaStatusUnScraped
	mediaFile.MediaSeason.Save()
	helpers.AppLogger.Infof("回滚电视剧 %s 季 %d 成功", mediaFile.Name, mediaFile.SeasonNumber)
	return nil
}

func (t *tvShowScrapeImpl) UpdateSeasonPathAndIdToAllEpisode(mediaFile *models.ScrapeMediaFile) error {
	updateData := map[string]interface{}{
		"path_id": mediaFile.PathId,
		"path":    mediaFile.Path,
	}
	affectedRows := db.Db.Table("scrape_media_files").Where("scrape_path_id =? AND media_season_id = ? AND batch_no = ?", mediaFile.ScrapePathId, mediaFile.MediaSeasonId, mediaFile.BatchNo).Updates(updateData).RowsAffected
	if affectedRows == 0 {
		helpers.AppLogger.Errorf("批量更新电视剧 %s 季 %d 的所有集的信息失败, 未更新任何行", mediaFile.Name, mediaFile.SeasonNumber)
		return errors.New("no rows affected")
	}
	helpers.AppLogger.Infof("批量更新电视剧 %s 季 %d 的所有集的信息成功，共更新 %d 行", mediaFile.Name, mediaFile.SeasonNumber, affectedRows)
	return nil
}
