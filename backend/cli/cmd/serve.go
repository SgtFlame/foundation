/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbhandlers"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice/impl"
	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
)

var (
	serveAddr   string
	serveKBRoot string
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var kbOpts []impl.Option
		if serveKBRoot != "" {
			kbOpts = append(kbOpts, impl.WithRoot(serveKBRoot))
		}
		kbService := impl.CreateService(kbOpts...)

		router := gin.Default()
		api := router.Group("/api/v1")

		kbHandler := &kbhandlers.KnowledgeBaseHandler{}
		kbHandler.Register(api, kbService)

		return router.Run(serveAddr)
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// serveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	serveCmd.Flags().StringVar(&serveAddr, "addr", ":8080", "address to listen on")
	serveCmd.Flags().StringVar(&serveKBRoot, "kb-root", "", "absolute knowledge base root directory (default ~/.foundation/kb)")
}
